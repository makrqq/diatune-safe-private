from diatune_safe.config import AppSettings
from diatune_safe.domain import Recommendation
from diatune_safe.safety import SafetyPolicy


def _settings() -> AppSettings:
    return AppSettings(
        app_api_key="",
        database_path="/tmp/diatune-safe-test.sqlite3",
        max_daily_change_pct=4.0,
        safety_min_confidence=0.55,
        global_hypo_guard_limit=2,
    )


def test_clamps_change_to_daily_limit():
    policy = SafetyPolicy(_settings())
    rec = Recommendation(
        parameter="icr",
        block_name="08-11",
        current_value=10.0,
        proposed_value=7.0,
        percent_change=-30.0,
        confidence=0.95,
        rationale=["test"],
    )

    out = policy.apply(rec, global_hypos=0, block_hypos=0)
    assert out.proposed_value == 9.6
    assert round(out.percent_change, 1) == -4.0
    assert out.blocked is False


def test_blocks_aggressive_change_on_hypos():
    policy = SafetyPolicy(_settings())
    rec = Recommendation(
        parameter="basal",
        block_name="00-03",
        current_value=0.7,
        proposed_value=0.9,
        percent_change=25.0,
        confidence=0.9,
    )

    out = policy.apply(rec, global_hypos=0, block_hypos=1)
    assert out.blocked is True
    assert "гипо" in (out.blocked_reason or "").lower()


def test_blocks_low_confidence():
    policy = SafetyPolicy(_settings())
    rec = Recommendation(
        parameter="isf",
        block_name="12-15",
        current_value=40,
        proposed_value=42,
        percent_change=5.0,
        confidence=0.25,
    )
    out = policy.apply(rec, global_hypos=0, block_hypos=0)
    assert out.blocked is True
    assert "уверенность" in (out.blocked_reason or "").lower()
