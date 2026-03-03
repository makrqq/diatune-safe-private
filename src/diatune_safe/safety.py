from dataclasses import dataclass

from diatune_safe.config import AppSettings
from diatune_safe.domain import Recommendation


@dataclass(frozen=True)
class SafetyRanges:
    icr_min: float = 2.0
    icr_max: float = 40.0
    isf_min: float = 10.0
    isf_max: float = 300.0
    basal_min: float = 0.05
    basal_max: float = 5.0


class SafetyPolicy:
    def __init__(self, settings: AppSettings) -> None:
        self.settings = settings
        self.ranges = SafetyRanges()

    def apply(
        self,
        recommendation: Recommendation,
        *,
        global_hypos: int,
        block_hypos: int,
    ) -> Recommendation:
        if recommendation.confidence < self.settings.safety_min_confidence:
            recommendation.blocked = True
            recommendation.blocked_reason = "Низкая уверенность рекомендации."
            recommendation.rationale.append("Требуется больше данных для безопасной корректировки.")
            return recommendation

        if global_hypos >= self.settings.global_hypo_guard_limit:
            recommendation.blocked = True
            recommendation.blocked_reason = "Слишком много гипо за период анализа."
            recommendation.rationale.append("Алгоритм блокирует все ужесточающие изменения.")
            return recommendation

        if block_hypos > 0 and self._is_aggressive_change(recommendation):
            recommendation.blocked = True
            recommendation.blocked_reason = "Обнаружены гипо в блоке; ужесточение запрещено."
            recommendation.rationale.append("Допустимы только более мягкие изменения после ручного ревью.")
            return recommendation

        recommendation.proposed_value = self._clamp_by_parameter(recommendation.parameter, recommendation.proposed_value)
        recommendation.proposed_value = self._clamp_change(
            current_value=recommendation.current_value,
            proposed_value=recommendation.proposed_value,
            max_change_pct=self.settings.max_daily_change_pct,
        )
        recommendation.percent_change = (
            100.0 * (recommendation.proposed_value - recommendation.current_value) / recommendation.current_value
        )

        if abs(recommendation.percent_change) < 0.5:
            recommendation.blocked = True
            recommendation.blocked_reason = "Изменение слишком мало для практической пользы."

        return recommendation

    def _is_aggressive_change(self, recommendation: Recommendation) -> bool:
        if recommendation.parameter == "icr":
            return recommendation.proposed_value < recommendation.current_value
        if recommendation.parameter == "isf":
            return recommendation.proposed_value < recommendation.current_value
        if recommendation.parameter == "basal":
            return recommendation.proposed_value > recommendation.current_value
        return False

    def _clamp_change(self, *, current_value: float, proposed_value: float, max_change_pct: float) -> float:
        max_delta = current_value * (max_change_pct / 100.0)
        lower = current_value - max_delta
        upper = current_value + max_delta
        return min(max(proposed_value, lower), upper)

    def _clamp_by_parameter(self, parameter: str, value: float) -> float:
        if parameter == "icr":
            return min(max(value, self.ranges.icr_min), self.ranges.icr_max)
        if parameter == "isf":
            return min(max(value, self.ranges.isf_min), self.ranges.isf_max)
        if parameter == "basal":
            return min(max(value, self.ranges.basal_min), self.ranges.basal_max)
        return value
