from functools import lru_cache

from pydantic import Field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class AppSettings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", env_file_encoding="utf-8", extra="ignore")

    app_env: str = Field(default="development", alias="APP_ENV")
    app_host: str = Field(default="0.0.0.0", alias="APP_HOST")
    app_port: int = Field(default=8080, alias="APP_PORT")
    app_api_key: str = Field(default="", alias="APP_API_KEY")
    database_path: str = Field(default="/workspace/data/diatune_safe.sqlite3", alias="DATABASE_PATH")
    timezone: str = Field(default="UTC", alias="TIMEZONE")
    log_level: str = Field(default="INFO", alias="LOG_LEVEL")

    telegram_bot_token: str = Field(default="", alias="TELEGRAM_BOT_TOKEN")
    telegram_allowed_user_ids: list[int] = Field(default_factory=list, alias="TELEGRAM_ALLOWED_USER_IDS")

    nightscout_url: str = Field(default="", alias="NIGHTSCOUT_URL")
    nightscout_api_secret: str = Field(default="", alias="NIGHTSCOUT_API_SECRET")

    analysis_lookback_days: int = Field(default=14, alias="ANALYSIS_LOOKBACK_DAYS")
    max_daily_change_pct: float = Field(default=4.0, alias="MAX_DAILY_CHANGE_PCT")
    min_meals_per_block: int = Field(default=3, alias="MIN_MEALS_PER_BLOCK")
    min_corrections_per_block: int = Field(default=3, alias="MIN_CORRECTIONS_PER_BLOCK")
    min_fasting_hours: int = Field(default=6, alias="MIN_FASTING_HOURS")
    hypo_threshold_mgdl: int = Field(default=70, alias="HYPO_THRESHOLD_MGDL")
    hyper_threshold_mgdl: int = Field(default=180, alias="HYPER_THRESHOLD_MGDL")
    global_hypo_guard_limit: int = Field(default=2, alias="GLOBAL_HYPO_GUARD_LIMIT")
    safety_min_confidence: float = Field(default=0.55, alias="SAFETY_MIN_CONFIDENCE")
    auto_analysis_enabled: bool = Field(default=False, alias="AUTO_ANALYSIS_ENABLED")
    auto_analysis_interval_minutes: int = Field(default=360, alias="AUTO_ANALYSIS_INTERVAL_MINUTES")
    auto_analysis_patient_ids: list[str] = Field(default_factory=list, alias="AUTO_ANALYSIS_PATIENT_IDS")

    @field_validator("telegram_allowed_user_ids", mode="before")
    @classmethod
    def _parse_user_ids(cls, value: object) -> list[int]:
        if value is None:
            return []
        if isinstance(value, list):
            return [int(item) for item in value]
        raw = str(value).strip()
        if not raw:
            return []
        return [int(item.strip()) for item in raw.split(",") if item.strip()]

    @field_validator("auto_analysis_patient_ids", mode="before")
    @classmethod
    def _parse_patient_ids(cls, value: object) -> list[str]:
        if value is None:
            return []
        if isinstance(value, list):
            return [str(item).strip() for item in value if str(item).strip()]
        raw = str(value).strip()
        if not raw:
            return []
        return [item.strip() for item in raw.split(",") if item.strip()]


@lru_cache(maxsize=1)
def get_settings() -> AppSettings:
    return AppSettings()
