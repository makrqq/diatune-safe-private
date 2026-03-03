from __future__ import annotations

import json
import sqlite3
from contextlib import contextmanager
from datetime import UTC, datetime
from pathlib import Path
from typing import Iterator

from diatune_safe.domain import AnalysisReport, PatientProfile, Recommendation


def _utcnow_iso() -> str:
    return datetime.now(UTC).isoformat()


def _parse_dt(value: str) -> datetime:
    dt = datetime.fromisoformat(value)
    if dt.tzinfo is None:
        return dt.replace(tzinfo=UTC)
    return dt


class SQLiteRepository:
    def __init__(self, db_path: str) -> None:
        self.db_path = db_path
        Path(db_path).parent.mkdir(parents=True, exist_ok=True)
        self._init_schema()

    @contextmanager
    def _connect(self) -> Iterator[sqlite3.Connection]:
        conn = sqlite3.connect(self.db_path)
        conn.row_factory = sqlite3.Row
        try:
            yield conn
            conn.commit()
        finally:
            conn.close()

    def _init_schema(self) -> None:
        with self._connect() as conn:
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS patient_profiles (
                  patient_id TEXT PRIMARY KEY,
                  profile_json TEXT NOT NULL,
                  created_at TEXT NOT NULL,
                  updated_at TEXT NOT NULL
                )
                """
            )
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS analysis_runs (
                  id INTEGER PRIMARY KEY AUTOINCREMENT,
                  patient_id TEXT NOT NULL,
                  generated_at TEXT NOT NULL,
                  period_start TEXT NOT NULL,
                  period_end TEXT NOT NULL,
                  global_hypo_events INTEGER NOT NULL,
                  warnings_json TEXT NOT NULL,
                  stats_json TEXT NOT NULL
                )
                """
            )
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS recommendations (
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
                  FOREIGN KEY (run_id) REFERENCES analysis_runs(id)
                )
                """
            )

    def upsert_profile(self, profile: PatientProfile) -> PatientProfile:
        now = _utcnow_iso()
        with self._connect() as conn:
            conn.execute(
                """
                INSERT INTO patient_profiles (patient_id, profile_json, created_at, updated_at)
                VALUES (?, ?, ?, ?)
                ON CONFLICT(patient_id) DO UPDATE SET
                  profile_json=excluded.profile_json,
                  updated_at=excluded.updated_at
                """,
                (profile.patient_id, profile.model_dump_json(), now, now),
            )
        return profile

    def get_profile(self, patient_id: str) -> PatientProfile | None:
        with self._connect() as conn:
            row = conn.execute(
                "SELECT profile_json FROM patient_profiles WHERE patient_id=?",
                (patient_id,),
            ).fetchone()
        if not row:
            return None
        return PatientProfile.model_validate_json(row["profile_json"])

    def save_report(self, report: AnalysisReport) -> AnalysisReport:
        with self._connect() as conn:
            cursor = conn.execute(
                """
                INSERT INTO analysis_runs (
                  patient_id, generated_at, period_start, period_end, global_hypo_events, warnings_json, stats_json
                ) VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    report.patient_id,
                    report.generated_at.isoformat(),
                    report.period_start.isoformat(),
                    report.period_end.isoformat(),
                    report.global_hypo_events,
                    json.dumps(report.warnings, ensure_ascii=False),
                    json.dumps([item.model_dump(mode="json") for item in report.stats], ensure_ascii=False),
                ),
            )
            run_id = int(cursor.lastrowid)
            saved_recommendations: list[Recommendation] = []
            for rec in report.recommendations:
                rec_cursor = conn.execute(
                    """
                    INSERT INTO recommendations (
                      run_id, parameter, block_name, current_value, proposed_value, percent_change,
                      confidence, blocked, blocked_reason, rationale_json
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                    """,
                    (
                        run_id,
                        rec.parameter,
                        rec.block_name,
                        rec.current_value,
                        rec.proposed_value,
                        rec.percent_change,
                        rec.confidence,
                        int(rec.blocked),
                        rec.blocked_reason,
                        json.dumps(rec.rationale, ensure_ascii=False),
                    ),
                )
                saved_recommendations.append(
                    rec.model_copy(update={"id": int(rec_cursor.lastrowid)})
                )
        return report.model_copy(update={"run_id": run_id, "recommendations": saved_recommendations})

    def list_report_ids(self, patient_id: str, limit: int = 20) -> list[int]:
        with self._connect() as conn:
            rows = conn.execute(
                """
                SELECT id FROM analysis_runs
                WHERE patient_id=?
                ORDER BY id DESC
                LIMIT ?
                """,
                (patient_id, limit),
            ).fetchall()
        return [int(row["id"]) for row in rows]

    def get_latest_report(self, patient_id: str) -> AnalysisReport | None:
        with self._connect() as conn:
            row = conn.execute(
                """
                SELECT id FROM analysis_runs
                WHERE patient_id=?
                ORDER BY id DESC
                LIMIT 1
                """,
                (patient_id,),
            ).fetchone()
        if not row:
            return None
        return self.get_report(int(row["id"]))

    def get_report(self, run_id: int) -> AnalysisReport | None:
        with self._connect() as conn:
            run = conn.execute(
                """
                SELECT *
                FROM analysis_runs
                WHERE id=?
                """,
                (run_id,),
            ).fetchone()
            if not run:
                return None
            rows = conn.execute(
                """
                SELECT *
                FROM recommendations
                WHERE run_id=?
                ORDER BY id ASC
                """,
                (run_id,),
            ).fetchall()
        recommendations = [
            Recommendation(
                id=int(row["id"]),
                parameter=row["parameter"],
                block_name=row["block_name"],
                current_value=float(row["current_value"]),
                proposed_value=float(row["proposed_value"]),
                percent_change=float(row["percent_change"]),
                confidence=float(row["confidence"]),
                blocked=bool(row["blocked"]),
                blocked_reason=row["blocked_reason"],
                rationale=json.loads(row["rationale_json"]),
            )
            for row in rows
        ]
        return AnalysisReport(
            run_id=int(run["id"]),
            patient_id=run["patient_id"],
            generated_at=_parse_dt(run["generated_at"]),
            period_start=_parse_dt(run["period_start"]),
            period_end=_parse_dt(run["period_end"]),
            global_hypo_events=int(run["global_hypo_events"]),
            warnings=json.loads(run["warnings_json"]),
            stats=json.loads(run["stats_json"]),
            recommendations=recommendations,
        )

    def list_pending_recommendations(self, patient_id: str, limit: int = 100) -> list[Recommendation]:
        with self._connect() as conn:
            rows = conn.execute(
                """
                SELECT r.*
                FROM recommendations r
                INNER JOIN analysis_runs a ON a.id=r.run_id
                WHERE a.patient_id=? AND r.blocked=0 AND r.acknowledged=0
                ORDER BY r.id DESC
                LIMIT ?
                """,
                (patient_id, limit),
            ).fetchall()

        pending: list[Recommendation] = []
        for row in rows:
            pending.append(
                Recommendation(
                    id=int(row["id"]),
                    parameter=row["parameter"],
                    block_name=row["block_name"],
                    current_value=float(row["current_value"]),
                    proposed_value=float(row["proposed_value"]),
                    percent_change=float(row["percent_change"]),
                    confidence=float(row["confidence"]),
                    blocked=False,
                    blocked_reason=row["blocked_reason"],
                    rationale=json.loads(row["rationale_json"]),
                )
            )
        return pending

    def acknowledge_recommendation(self, recommendation_id: int, reviewer: str) -> bool:
        with self._connect() as conn:
            result = conn.execute(
                """
                UPDATE recommendations
                SET acknowledged=1, acknowledged_at=?, acknowledged_by=?
                WHERE id=? AND acknowledged=0
                """,
                (_utcnow_iso(), reviewer, recommendation_id),
            )
            return result.rowcount > 0
