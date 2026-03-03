from __future__ import annotations

import asyncio
import logging

from apscheduler.schedulers.asyncio import AsyncIOScheduler
from apscheduler.triggers.interval import IntervalTrigger

from diatune_safe.config import AppSettings
from diatune_safe.service import AnalysisService

logger = logging.getLogger(__name__)


class AnalysisScheduler:
    def __init__(self, settings: AppSettings, service: AnalysisService) -> None:
        self.settings = settings
        self.service = service
        self.scheduler = AsyncIOScheduler(timezone=settings.timezone)

    async def run_forever(self, patient_ids: list[str] | None = None) -> None:
        ids = patient_ids or self.settings.auto_analysis_patient_ids
        if not ids:
            raise RuntimeError("No patient ids provided for scheduler.")

        for patient_id in ids:
            self.scheduler.add_job(
                self._analyze_patient,
                trigger=IntervalTrigger(minutes=self.settings.auto_analysis_interval_minutes),
                kwargs={"patient_id": patient_id},
                max_instances=1,
                coalesce=True,
                id=f"analyze-{patient_id}",
                replace_existing=True,
            )
            logger.info("Scheduled background analysis for patient_id=%s", patient_id)

        self.scheduler.start()
        try:
            while True:
                await asyncio.sleep(3600)
        finally:
            self.scheduler.shutdown(wait=False)

    async def _analyze_patient(self, patient_id: str) -> None:
        try:
            report = await self.service.run_analysis(
                patient_id=patient_id,
                days=self.settings.analysis_lookback_days,
                prefer_real_data=True,
            )
            logger.info(
                "Scheduled analysis complete patient_id=%s run_id=%s warnings=%s",
                patient_id,
                report.run_id,
                len(report.warnings),
            )
        except Exception:
            logger.exception("Scheduled analysis failed for patient_id=%s", patient_id)
