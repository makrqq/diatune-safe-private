from __future__ import annotations

from typing import Annotated

from fastapi import Depends, FastAPI, Header, HTTPException, Query, status
from pydantic import BaseModel, Field

from diatune_safe.config import AppSettings
from diatune_safe.domain import AnalysisReport, PatientProfile, Recommendation
from diatune_safe.service import AnalysisService


class HealthResponse(BaseModel):
    status: str
    mode: str


class AcknowledgeRequest(BaseModel):
    reviewer: str = Field(min_length=2, max_length=80)


class AcknowledgeResponse(BaseModel):
    acknowledged: bool


class ReportListResponse(BaseModel):
    reports: list[AnalysisReport]


class RecommendationListResponse(BaseModel):
    recommendations: list[Recommendation]


def create_app(settings: AppSettings, service: AnalysisService | None = None) -> FastAPI:
    app = FastAPI(
        title="Diatune Safe API",
        version="0.1.0",
        description=(
            "Safety-first recommendation platform for Type 1 diabetes profile tuning. "
            "The system does NOT apply settings automatically."
        ),
    )
    app.state.settings = settings
    app.state.service = service or AnalysisService(settings)

    def get_service() -> AnalysisService:
        return app.state.service

    def verify_api_key(x_api_key: Annotated[str | None, Header()] = None) -> None:
        configured = app.state.settings.app_api_key
        if not configured:
            return
        if x_api_key != configured:
            raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="Invalid API key")

    @app.get("/healthz", response_model=HealthResponse, dependencies=[Depends(verify_api_key)])
    async def healthz() -> HealthResponse:
        mode = "nightscout" if app.state.settings.nightscout_url else "synthetic"
        return HealthResponse(status="ok", mode=mode)

    @app.get("/v1/patients/{patient_id}/profile", response_model=PatientProfile, dependencies=[Depends(verify_api_key)])
    async def get_profile(patient_id: str, service: AnalysisService = Depends(get_service)) -> PatientProfile:
        return service.get_profile(patient_id)

    @app.put("/v1/patients/{patient_id}/profile", response_model=PatientProfile, dependencies=[Depends(verify_api_key)])
    async def save_profile(
        patient_id: str, profile: PatientProfile, service: AnalysisService = Depends(get_service)
    ) -> PatientProfile:
        return service.save_profile(patient_id, profile)

    @app.post("/v1/patients/{patient_id}/analyze", response_model=AnalysisReport, dependencies=[Depends(verify_api_key)])
    async def analyze(
        patient_id: str,
        service: AnalysisService = Depends(get_service),
        days: int = Query(default=14, ge=1, le=90),
        prefer_real_data: bool = Query(default=True),
    ) -> AnalysisReport:
        return await service.run_analysis(patient_id=patient_id, days=days, prefer_real_data=prefer_real_data)

    @app.get(
        "/v1/patients/{patient_id}/reports/latest",
        response_model=AnalysisReport,
        dependencies=[Depends(verify_api_key)],
    )
    async def latest_report(patient_id: str, service: AnalysisService = Depends(get_service)) -> AnalysisReport:
        report = service.get_latest_report(patient_id)
        if not report:
            raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="No report found.")
        return report

    @app.get(
        "/v1/patients/{patient_id}/reports",
        response_model=ReportListResponse,
        dependencies=[Depends(verify_api_key)],
    )
    async def list_reports(
        patient_id: str,
        limit: int = Query(default=20, ge=1, le=100),
        service: AnalysisService = Depends(get_service),
    ) -> ReportListResponse:
        return ReportListResponse(reports=service.list_reports(patient_id, limit))

    @app.get(
        "/v1/patients/{patient_id}/recommendations/pending",
        response_model=RecommendationListResponse,
        dependencies=[Depends(verify_api_key)],
    )
    async def list_pending(patient_id: str, service: AnalysisService = Depends(get_service)) -> RecommendationListResponse:
        return RecommendationListResponse(recommendations=service.list_pending_recommendations(patient_id))

    @app.post(
        "/v1/recommendations/{recommendation_id}/acknowledge",
        response_model=AcknowledgeResponse,
        dependencies=[Depends(verify_api_key)],
    )
    async def acknowledge(
        recommendation_id: int, payload: AcknowledgeRequest, service: AnalysisService = Depends(get_service)
    ) -> AcknowledgeResponse:
        ok = service.acknowledge_recommendation(recommendation_id, payload.reviewer)
        if not ok:
            raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Recommendation not found.")
        return AcknowledgeResponse(acknowledged=True)

    return app
