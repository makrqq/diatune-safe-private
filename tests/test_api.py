from fastapi.testclient import TestClient

from diatune_safe.api import create_app
from diatune_safe.config import AppSettings
from diatune_safe.service import AnalysisService


def test_api_flow(tmp_path):
    db_path = tmp_path / "api.sqlite3"
    settings = AppSettings(
        app_api_key="secret",
        database_path=str(db_path),
        min_meals_per_block=1,
        min_corrections_per_block=1,
        min_fasting_hours=1,
        safety_min_confidence=0.0,
        global_hypo_guard_limit=99,
    )
    service = AnalysisService(settings)
    app = create_app(settings=settings, service=service)
    client = TestClient(app)
    headers = {"X-API-Key": "secret"}

    health = client.get("/healthz", headers=headers)
    assert health.status_code == 200
    assert health.json()["status"] == "ok"

    analyze = client.post("/v1/patients/demo/analyze?days=2&prefer_real_data=false", headers=headers)
    assert analyze.status_code == 200
    run_id = analyze.json()["run_id"]
    assert run_id is not None

    latest = client.get("/v1/patients/demo/reports/latest", headers=headers)
    assert latest.status_code == 200
    assert latest.json()["run_id"] == run_id

    pending = client.get("/v1/patients/demo/recommendations/pending", headers=headers)
    assert pending.status_code == 200
