package service

import (
	"context"
	"log"
	"time"

	"diatune-safe/internal/config"
	"diatune-safe/internal/datasource"
	"diatune-safe/internal/domain"
	"diatune-safe/internal/engine"
	"diatune-safe/internal/repository"
)

type Service struct {
	settings   config.Settings
	repo       *repository.SQLiteRepository
	engine     engine.Engine
	synthetic  datasource.Source
	nightscout datasource.Source
}

func New(settings config.Settings) (*Service, error) {
	repo, err := repository.NewSQLite(settings.DatabasePath)
	if err != nil {
		return nil, err
	}
	var nightscout datasource.Source
	if settings.NightscoutURL != "" {
		nightscout = datasource.NightscoutSource{
			BaseURL:   settings.NightscoutURL,
			APISecret: settings.NightscoutAPISecret,
			Timeout:   30 * time.Second,
		}
	}
	return &Service{
		settings:   settings,
		repo:       repo,
		engine:     engine.New(settings),
		synthetic:  datasource.SyntheticSource{Seed: 7},
		nightscout: nightscout,
	}, nil
}

func (s *Service) Close() error {
	return s.repo.Close()
}

func (s *Service) GetProfile(patientID string) (domain.PatientProfile, error) {
	p, err := s.repo.GetProfile(patientID)
	if err != nil {
		return domain.PatientProfile{}, err
	}
	if p != nil {
		return *p, nil
	}
	profile := s.defaultProfile(patientID)
	if _, err := s.repo.UpsertProfile(profile); err != nil {
		return domain.PatientProfile{}, err
	}
	return profile, nil
}

func (s *Service) SaveProfile(patientID string, profile domain.PatientProfile) (domain.PatientProfile, error) {
	if profile.PatientID != patientID {
		profile.PatientID = patientID
	}
	return s.repo.UpsertProfile(profile)
}

func (s *Service) RunAnalysis(ctx context.Context, patientID string, days int, preferRealData bool) (domain.AnalysisReport, error) {
	if days <= 0 {
		days = s.settings.AnalysisLookbackDays
	}
	periodEnd := time.Now().UTC()
	periodStart := periodEnd.Add(-time.Duration(days) * 24 * time.Hour)

	profile, err := s.GetProfile(patientID)
	if err != nil {
		return domain.AnalysisReport{}, err
	}
	dataset, _, err := s.loadDatasetWithSource(ctx, patientID, periodStart, periodEnd, preferRealData)
	if err != nil {
		return domain.AnalysisReport{}, err
	}

	report := s.engine.Analyze(patientID, profile, dataset, periodStart, periodEnd)
	saved, err := s.repo.SaveReport(report)
	if err != nil {
		return domain.AnalysisReport{}, err
	}
	log.Printf("анализ завершен patient_id=%s run_id=%v", patientID, saved.RunID)
	return saved, nil
}

func (s *Service) GetLatestReport(patientID string) (*domain.AnalysisReport, error) {
	return s.repo.GetLatestReport(patientID)
}

func (s *Service) ListReports(patientID string, limit int) ([]domain.AnalysisReport, error) {
	ids, err := s.repo.ListReportIDs(patientID, limit)
	if err != nil {
		return nil, err
	}
	out := []domain.AnalysisReport{}
	for _, id := range ids {
		report, err := s.repo.GetReport(id)
		if err != nil {
			return nil, err
		}
		if report != nil {
			out = append(out, *report)
		}
	}
	return out, nil
}

func (s *Service) ListPendingRecommendations(patientID string, limit int) ([]domain.Recommendation, error) {
	if limit <= 0 {
		limit = 100
	}
	return s.repo.ListPendingRecommendations(patientID, limit)
}

func (s *Service) AcknowledgeRecommendation(recommendationID int64, reviewer string) (bool, error) {
	return s.repo.AcknowledgeRecommendation(recommendationID, reviewer)
}

func (s *Service) loadDatasetWithSource(ctx context.Context, patientID string, periodStart, periodEnd time.Time, preferReal bool) (domain.PatientDataset, string, error) {
	if preferReal && s.nightscout != nil {
		dataset, err := s.nightscout.FetchDataset(ctx, patientID, periodStart, periodEnd)
		if err == nil && len(dataset.Glucose) > 0 {
			return dataset, "nightscout", nil
		}
		if err != nil {
			log.Printf("ошибка загрузки из Nightscout (%v), используем синтетические данные", err)
		} else {
			log.Printf("Nightscout вернул пустой набор глюкозы, используем синтетические данные")
		}
	}
	dataset, err := s.synthetic.FetchDataset(ctx, patientID, periodStart, periodEnd)
	if err != nil {
		return domain.PatientDataset{}, "", err
	}
	return dataset, "synthetic", nil
}

func (s *Service) loadDataset(ctx context.Context, patientID string, periodStart, periodEnd time.Time, preferReal bool) (domain.PatientDataset, error) {
	dataset, _, err := s.loadDatasetWithSource(ctx, patientID, periodStart, periodEnd, preferReal)
	return dataset, err
}

func (s *Service) defaultProfile(patientID string) domain.PatientProfile {
	return domain.PatientProfile{
		PatientID:      patientID,
		Timezone:       s.settings.Timezone,
		TargetLowMgdl:  90,
		TargetHighMgdl: 130,
		Blocks: []domain.BlockSettings{
			{Block: domain.TimeBlock{Name: "00-03", StartHour: 0, EndHour: 3}, ICR: 12, ISF: 55, Basal: 0.70},
			{Block: domain.TimeBlock{Name: "04-07", StartHour: 4, EndHour: 7}, ICR: 10, ISF: 45, Basal: 0.85},
			{Block: domain.TimeBlock{Name: "08-11", StartHour: 8, EndHour: 11}, ICR: 9, ISF: 40, Basal: 0.80},
			{Block: domain.TimeBlock{Name: "12-15", StartHour: 12, EndHour: 15}, ICR: 10, ISF: 42, Basal: 0.78},
			{Block: domain.TimeBlock{Name: "16-19", StartHour: 16, EndHour: 19}, ICR: 9, ISF: 40, Basal: 0.83},
			{Block: domain.TimeBlock{Name: "20-23", StartHour: 20, EndHour: 23}, ICR: 11, ISF: 48, Basal: 0.72},
		},
	}
}
