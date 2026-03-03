from datetime import UTC, datetime, timedelta

from diatune_safe.config import AppSettings
from diatune_safe.domain import (
    BlockSettings,
    CarbEvent,
    GlucosePoint,
    InsulinEvent,
    PatientDataset,
    PatientProfile,
    TimeBlock,
)
from diatune_safe.engine import RecommendationEngine


def _settings() -> AppSettings:
    return AppSettings(
        app_api_key="",
        database_path="/tmp/diatune-safe-engine.sqlite3",
        min_meals_per_block=2,
        min_corrections_per_block=2,
        min_fasting_hours=2,
        max_daily_change_pct=4.0,
        safety_min_confidence=0.2,
        global_hypo_guard_limit=20,
    )


def _profile() -> PatientProfile:
    return PatientProfile(
        patient_id="demo",
        blocks=[
            BlockSettings(block=TimeBlock(name="00-23", start_hour=0, end_hour=23), icr=10, isf=45, basal=0.8),
        ],
    )


def test_engine_produces_recommendations():
    start = datetime(2026, 2, 1, tzinfo=UTC)
    glucose = []
    for i in range(0, 24 * 12):
        ts = start + timedelta(minutes=5 * i)
        glucose.append(GlucosePoint(ts=ts, mgdl=110 + (i % 12)))

    meals = [
        CarbEvent(ts=start + timedelta(hours=8), grams=45),
        CarbEvent(ts=start + timedelta(hours=13), grams=50),
        CarbEvent(ts=start + timedelta(hours=19), grams=55),
    ]
    insulin = [
        InsulinEvent(ts=start + timedelta(hours=8) - timedelta(minutes=10), units=4.6, kind="bolus"),
        InsulinEvent(ts=start + timedelta(hours=13) - timedelta(minutes=10), units=4.8, kind="bolus"),
        InsulinEvent(ts=start + timedelta(hours=19) - timedelta(minutes=10), units=5.4, kind="bolus"),
        InsulinEvent(ts=start + timedelta(hours=4), units=1.2, kind="bolus"),
        InsulinEvent(ts=start + timedelta(hours=16), units=1.0, kind="bolus"),
    ]
    dataset = PatientDataset(glucose=glucose, carbs=meals, insulin=insulin)
    engine = RecommendationEngine(_settings())

    report = engine.analyze(
        patient_id="demo",
        profile=_profile(),
        dataset=dataset,
        period_start=start,
        period_end=start + timedelta(days=1),
    )

    assert len(report.recommendations) == 3
    assert any(rec.parameter == "icr" for rec in report.recommendations)
    assert any(rec.parameter == "isf" for rec in report.recommendations)
    assert any(rec.parameter == "basal" for rec in report.recommendations)
