package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"diatune-safe/internal/domain"

	_ "modernc.org/sqlite"
)

type SQLiteRepository struct {
	db *sql.DB
}

func NewSQLite(path string) (*SQLiteRepository, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	repo := &SQLiteRepository{db: db}
	if err := repo.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return repo, nil
}

func (r *SQLiteRepository) Close() error {
	return r.db.Close()
}

func (r *SQLiteRepository) initSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS patient_profiles (
			patient_id TEXT PRIMARY KEY,
			profile_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS analysis_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			patient_id TEXT NOT NULL,
			generated_at TEXT NOT NULL,
			period_start TEXT NOT NULL,
			period_end TEXT NOT NULL,
			global_hypo_events INTEGER NOT NULL,
			warnings_json TEXT NOT NULL,
			stats_json TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS recommendations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id INTEGER NOT NULL,
			parameter TEXT NOT NULL,
			block_name TEXT NOT NULL,
			current_value REAL NOT NULL,
			proposed_value REAL NOT NULL,
			percent_change REAL NOT NULL,
			confidence REAL NOT NULL,
			blocked INTEGER NOT NULL,
			blocked_reason TEXT,
			rationale_json TEXT NOT NULL,
			acknowledged INTEGER NOT NULL DEFAULT 0,
			acknowledged_at TEXT,
			acknowledged_by TEXT,
			FOREIGN KEY(run_id) REFERENCES analysis_runs(id)
		);`,
	}
	for _, stmt := range stmts {
		if _, err := r.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (r *SQLiteRepository) UpsertProfile(profile domain.PatientProfile) (domain.PatientProfile, error) {
	raw, err := json.Marshal(profile)
	if err != nil {
		return domain.PatientProfile{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = r.db.Exec(`
		INSERT INTO patient_profiles (patient_id, profile_json, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(patient_id) DO UPDATE SET
		profile_json=excluded.profile_json,
		updated_at=excluded.updated_at
	`, profile.PatientID, string(raw), now, now)
	if err != nil {
		return domain.PatientProfile{}, err
	}
	return profile, nil
}

func (r *SQLiteRepository) GetProfile(patientID string) (*domain.PatientProfile, error) {
	row := r.db.QueryRow(`SELECT profile_json FROM patient_profiles WHERE patient_id=?`, patientID)
	var raw string
	if err := row.Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	var p domain.PatientProfile
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *SQLiteRepository) SaveReport(report domain.AnalysisReport) (domain.AnalysisReport, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return domain.AnalysisReport{}, err
	}
	defer func() { _ = tx.Rollback() }()

	warningsRaw, _ := json.Marshal(report.Warnings)
	statsRaw, _ := json.Marshal(report.Stats)

	res, err := tx.Exec(`
		INSERT INTO analysis_runs (
			patient_id, generated_at, period_start, period_end, global_hypo_events, warnings_json, stats_json
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, report.PatientID, report.GeneratedAt.Format(time.RFC3339), report.PeriodStart.Format(time.RFC3339),
		report.PeriodEnd.Format(time.RFC3339), report.GlobalHypoEvents, string(warningsRaw), string(statsRaw))
	if err != nil {
		return domain.AnalysisReport{}, err
	}
	runID, err := res.LastInsertId()
	if err != nil {
		return domain.AnalysisReport{}, err
	}

	saved := make([]domain.Recommendation, 0, len(report.Recommendations))
	for _, rec := range report.Recommendations {
		rationaleRaw, _ := json.Marshal(rec.Rationale)
		blocked := 0
		if rec.Blocked {
			blocked = 1
		}
		rr, err := tx.Exec(`
			INSERT INTO recommendations (
				run_id, parameter, block_name, current_value, proposed_value, percent_change,
				confidence, blocked, blocked_reason, rationale_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, runID, string(rec.Parameter), rec.BlockName, rec.CurrentValue, rec.ProposedValue,
			rec.PercentChange, rec.Confidence, blocked, nullableString(rec.BlockedReason), string(rationaleRaw))
		if err != nil {
			return domain.AnalysisReport{}, err
		}
		recID, err := rr.LastInsertId()
		if err != nil {
			return domain.AnalysisReport{}, err
		}
		rec.ID = ptrInt64(recID)
		saved = append(saved, rec)
	}

	if err := tx.Commit(); err != nil {
		return domain.AnalysisReport{}, err
	}
	report.RunID = ptrInt64(runID)
	report.Recommendations = saved
	return report, nil
}

func (r *SQLiteRepository) ListReportIDs(patientID string, limit int) ([]int64, error) {
	rows, err := r.db.Query(`
		SELECT id FROM analysis_runs WHERE patient_id=? ORDER BY id DESC LIMIT ?
	`, patientID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *SQLiteRepository) GetLatestReport(patientID string) (*domain.AnalysisReport, error) {
	row := r.db.QueryRow(`SELECT id FROM analysis_runs WHERE patient_id=? ORDER BY id DESC LIMIT 1`, patientID)
	var id int64
	if err := row.Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return r.GetReport(id)
}

func (r *SQLiteRepository) GetReport(runID int64) (*domain.AnalysisReport, error) {
	row := r.db.QueryRow(`
		SELECT id, patient_id, generated_at, period_start, period_end, global_hypo_events, warnings_json, stats_json
		FROM analysis_runs WHERE id=?
	`, runID)
	var (
		id, globalHypos                                                    int64
		patientID, generatedAtRaw, startRaw, endRaw, warningsRaw, statsRaw string
	)
	if err := row.Scan(&id, &patientID, &generatedAtRaw, &startRaw, &endRaw, &globalHypos, &warningsRaw, &statsRaw); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	generatedAt, err := time.Parse(time.RFC3339, generatedAtRaw)
	if err != nil {
		return nil, err
	}
	periodStart, err := time.Parse(time.RFC3339, startRaw)
	if err != nil {
		return nil, err
	}
	periodEnd, err := time.Parse(time.RFC3339, endRaw)
	if err != nil {
		return nil, err
	}

	warnings := []string{}
	if err := json.Unmarshal([]byte(warningsRaw), &warnings); err != nil {
		return nil, err
	}
	stats := []domain.BlockStats{}
	if err := json.Unmarshal([]byte(statsRaw), &stats); err != nil {
		return nil, err
	}

	recs, err := r.getRecommendationsByRunID(id)
	if err != nil {
		return nil, err
	}

	report := domain.AnalysisReport{
		RunID:            ptrInt64(id),
		PatientID:        patientID,
		GeneratedAt:      generatedAt,
		PeriodStart:      periodStart,
		PeriodEnd:        periodEnd,
		GlobalHypoEvents: int(globalHypos),
		Warnings:         warnings,
		Stats:            stats,
		Recommendations:  recs,
	}
	return &report, nil
}

func (r *SQLiteRepository) getRecommendationsByRunID(runID int64) ([]domain.Recommendation, error) {
	rows, err := r.db.Query(`
		SELECT id, parameter, block_name, current_value, proposed_value, percent_change,
		       confidence, blocked, blocked_reason, rationale_json
		FROM recommendations WHERE run_id=? ORDER BY id ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.Recommendation{}
	for rows.Next() {
		var (
			id                                                     int64
			parameter, blockName, rationaleRaw                     string
			blockedReason                                          sql.NullString
			currentValue, proposedValue, percentChange, confidence float64
			blocked                                                int
		)
		if err := rows.Scan(&id, &parameter, &blockName, &currentValue, &proposedValue, &percentChange,
			&confidence, &blocked, &blockedReason, &rationaleRaw); err != nil {
			return nil, err
		}
		rationale := []string{}
		if err := json.Unmarshal([]byte(rationaleRaw), &rationale); err != nil {
			return nil, err
		}
		out = append(out, domain.Recommendation{
			ID:            ptrInt64(id),
			Parameter:     domain.ParameterName(parameter),
			BlockName:     blockName,
			CurrentValue:  currentValue,
			ProposedValue: proposedValue,
			PercentChange: percentChange,
			Confidence:    confidence,
			Blocked:       blocked == 1,
			BlockedReason: blockedReason.String,
			Rationale:     rationale,
		})
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) ListPendingRecommendations(patientID string, limit int) ([]domain.Recommendation, error) {
	rows, err := r.db.Query(`
		SELECT r.id, r.parameter, r.block_name, r.current_value, r.proposed_value, r.percent_change,
		       r.confidence, r.blocked_reason, r.rationale_json
		FROM recommendations r
		INNER JOIN analysis_runs a ON a.id=r.run_id
		WHERE a.patient_id=? AND r.blocked=0 AND r.acknowledged=0
		ORDER BY r.id DESC
		LIMIT ?
	`, patientID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.Recommendation{}
	for rows.Next() {
		var (
			id                                                     int64
			parameter, blockName, rationaleRaw                     string
			blockedReason                                          sql.NullString
			currentValue, proposedValue, percentChange, confidence float64
		)
		if err := rows.Scan(&id, &parameter, &blockName, &currentValue, &proposedValue, &percentChange, &confidence, &blockedReason, &rationaleRaw); err != nil {
			return nil, err
		}
		rationale := []string{}
		if err := json.Unmarshal([]byte(rationaleRaw), &rationale); err != nil {
			return nil, err
		}
		out = append(out, domain.Recommendation{
			ID:            ptrInt64(id),
			Parameter:     domain.ParameterName(parameter),
			BlockName:     blockName,
			CurrentValue:  currentValue,
			ProposedValue: proposedValue,
			PercentChange: percentChange,
			Confidence:    confidence,
			Blocked:       false,
			BlockedReason: blockedReason.String,
			Rationale:     rationale,
		})
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) AcknowledgeRecommendation(recommendationID int64, reviewer string) (bool, error) {
	res, err := r.db.Exec(`
		UPDATE recommendations
		SET acknowledged=1, acknowledged_at=?, acknowledged_by=?
		WHERE id=? AND acknowledged=0
	`, time.Now().UTC().Format(time.RFC3339), reviewer, recommendationID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func ptrInt64(v int64) *int64 {
	return &v
}

func Must(err error) {
	if err != nil {
		panic(fmt.Sprintf("repository error: %v", err))
	}
}
