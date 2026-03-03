from __future__ import annotations

from datetime import datetime
from typing import Literal

from pydantic import BaseModel, Field, model_validator


ParameterName = Literal["icr", "isf", "basal"]


class TimeBlock(BaseModel):
    name: str
    start_hour: int = Field(ge=0, le=23)
    end_hour: int = Field(ge=0, le=23)

    def contains_hour(self, hour: int) -> bool:
        if self.start_hour <= self.end_hour:
            return self.start_hour <= hour <= self.end_hour
        return hour >= self.start_hour or hour <= self.end_hour


class BlockSettings(BaseModel):
    block: TimeBlock
    icr: float = Field(gt=0)  # grams per 1u
    isf: float = Field(gt=0)  # mg/dL per 1u
    basal: float = Field(gt=0)  # units/hour


class PatientProfile(BaseModel):
    patient_id: str
    timezone: str = "UTC"
    target_low_mgdl: int = Field(default=90, ge=60, le=130)
    target_high_mgdl: int = Field(default=130, ge=80, le=180)
    blocks: list[BlockSettings]

    @model_validator(mode="after")
    def _validate_targets(self) -> "PatientProfile":
        if self.target_low_mgdl >= self.target_high_mgdl:
            raise ValueError("target_low_mgdl must be lower than target_high_mgdl")
        return self


class GlucosePoint(BaseModel):
    ts: datetime
    mgdl: float = Field(ge=20, le=500)


class CarbEvent(BaseModel):
    ts: datetime
    grams: float = Field(gt=0, le=400)


class InsulinEvent(BaseModel):
    ts: datetime
    units: float = Field(gt=0, le=30)
    kind: Literal["bolus", "basal"] = "bolus"


class PatientDataset(BaseModel):
    glucose: list[GlucosePoint] = Field(default_factory=list)
    carbs: list[CarbEvent] = Field(default_factory=list)
    insulin: list[InsulinEvent] = Field(default_factory=list)


class BlockStats(BaseModel):
    block_name: str
    meals: int = 0
    corrections: int = 0
    fasting_hours: float = 0.0
    hypo_events: int = 0
    hyper_events: int = 0
    mean_postprandial_delta: float | None = None
    mean_correction_ratio: float | None = None
    fasting_drift_mgdl_per_hour: float | None = None
    postprandial_variability: float | None = None
    correction_variability: float | None = None
    fasting_samples: int = 0


class Recommendation(BaseModel):
    id: int | None = None
    parameter: ParameterName
    block_name: str
    current_value: float
    proposed_value: float
    percent_change: float
    confidence: float = Field(ge=0, le=1)
    blocked: bool = False
    blocked_reason: str | None = None
    rationale: list[str] = Field(default_factory=list)


class AnalysisReport(BaseModel):
    run_id: int | None = None
    patient_id: str
    generated_at: datetime
    period_start: datetime
    period_end: datetime
    global_hypo_events: int = 0
    warnings: list[str] = Field(default_factory=list)
    stats: list[BlockStats] = Field(default_factory=list)
    recommendations: list[Recommendation] = Field(default_factory=list)
