from __future__ import annotations

import logging
from datetime import UTC, datetime, timedelta

from diatune_safe.config import AppSettings
from diatune_safe.data_sources import DataSourceError, NightscoutDataSource, SyntheticDataSource
from diatune_safe.domain import AnalysisReport, BlockSettings, PatientProfile, TimeBlock
from diatune_safe.engine import RecommendationEngine
from diatune_safe.repository import SQLiteRepository

logger = logging.getLogger(__name__)


class AnalysisService:
    def __init__(self, settings: AppSettings) -> None:
        self.settings = settings
        self.repository = SQLiteRepository(settings.database_path)
        self.engine = RecommendationEngine(settings)
        self.synthetic_data_source = SyntheticDataSource()
        self.nightscout_data_source = (
            NightscoutDataSource(base_url=settings.nightscout_url, api_secret=settings.nightscout_api_secret)
            if settings.nightscout_url
            else None
        )

    def get_profile(self, patient_id: str) -> PatientProfile:
        profile = self.repository.get_profile(patient_id)
        if profile:
            return profile
        profile = self._default_profile(patient_id)
        self.repository.upsert_profile(profile)
        return profile

    def save_profile(self, patient_id: str, profile: PatientProfile) -> PatientProfile:
        if profile.patient_id != patient_id:
            profile = profile.model_copy(update={"patient_id": patient_id})
        return self.repository.upsert_profile(profile)

    async def run_analysis(
        self,
        *,
        patient_id: str,
        days: int | None = None,
        prefer_real_data: bool = True,
    ) -> AnalysisReport:
        lookback = days if days and days > 0 else self.settings.analysis_lookback_days
        period_end = datetime.now(UTC)
        period_start = period_end - timedelta(days=lookback)
        profile = self.get_profile(patient_id)

        dataset = await self._load_dataset(
            patient_id=patient_id,
            period_start=period_start,
            period_end=period_end,
            prefer_real_data=prefer_real_data,
        )

        report = self.engine.analyze(
            patient_id=patient_id,
            profile=profile,
            dataset=dataset,
            period_start=period_start,
            period_end=period_end,
        )
        saved = self.repository.save_report(report)
        logger.info("Анализ завершен для patient_id=%s run_id=%s", patient_id, saved.run_id)
        return saved

    def get_latest_report(self, patient_id: str) -> AnalysisReport | None:
        return self.repository.get_latest_report(patient_id)

    def list_reports(self, patient_id: str, limit: int = 20) -> list[AnalysisReport]:
        report_ids = self.repository.list_report_ids(patient_id=patient_id, limit=limit)
        reports = [self.repository.get_report(run_id) for run_id in report_ids]
        return [report for report in reports if report is not None]

    def list_pending_recommendations(self, patient_id: str):
        return self.repository.list_pending_recommendations(patient_id)

    def acknowledge_recommendation(self, recommendation_id: int, reviewer: str) -> bool:
        return self.repository.acknowledge_recommendation(recommendation_id=recommendation_id, reviewer=reviewer)

    async def _load_dataset(
        self,
        *,
        patient_id: str,
        period_start: datetime,
        period_end: datetime,
        prefer_real_data: bool,
    ):
        if prefer_real_data and self.nightscout_data_source:
            try:
                dataset = await self.nightscout_data_source.fetch_dataset(
                    patient_id=patient_id,
                    since=period_start,
                    until=period_end,
                )
                if dataset.glucose:
                    return dataset
                logger.warning("Nightscout вернул пустой набор глюкозы; используем синтетические данные.")
            except DataSourceError as exc:
                logger.exception("Ошибка загрузки из Nightscout (%s), используем синтетические данные.", exc)
        return await self.synthetic_data_source.fetch_dataset(
            patient_id=patient_id,
            since=period_start,
            until=period_end,
        )

    def _default_profile(self, patient_id: str) -> PatientProfile:
        return PatientProfile(
            patient_id=patient_id,
            timezone=self.settings.timezone,
            target_low_mgdl=90,
            target_high_mgdl=130,
            blocks=[
                BlockSettings(block=TimeBlock(name="00-03", start_hour=0, end_hour=3), icr=12, isf=55, basal=0.70),
                BlockSettings(block=TimeBlock(name="04-07", start_hour=4, end_hour=7), icr=10, isf=45, basal=0.85),
                BlockSettings(block=TimeBlock(name="08-11", start_hour=8, end_hour=11), icr=9, isf=40, basal=0.80),
                BlockSettings(block=TimeBlock(name="12-15", start_hour=12, end_hour=15), icr=10, isf=42, basal=0.78),
                BlockSettings(block=TimeBlock(name="16-19", start_hour=16, end_hour=19), icr=9, isf=40, basal=0.83),
                BlockSettings(block=TimeBlock(name="20-23", start_hour=20, end_hour=23), icr=11, isf=48, basal=0.72),
            ],
        )
