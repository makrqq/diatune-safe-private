from __future__ import annotations

from collections import defaultdict
from datetime import UTC, datetime, timedelta

from diatune_safe.config import AppSettings
from diatune_safe.domain import (
    AnalysisReport,
    BlockStats,
    CarbEvent,
    GlucosePoint,
    InsulinEvent,
    PatientDataset,
    PatientProfile,
    Recommendation,
)
from diatune_safe.safety import SafetyPolicy
from diatune_safe.stats_utils import mad, robust_mean


class RecommendationEngine:
    def __init__(self, settings: AppSettings) -> None:
        self.settings = settings
        self.safety = SafetyPolicy(settings)

    def analyze(
        self,
        *,
        patient_id: str,
        profile: PatientProfile,
        dataset: PatientDataset,
        period_start: datetime,
        period_end: datetime,
    ) -> AnalysisReport:
        normalized = self._normalize_dataset(dataset)
        stats, global_hypos = self._build_stats(profile=profile, dataset=normalized)
        warnings = self._build_warnings(stats=stats, global_hypos=global_hypos)
        recommendations = self._build_recommendations(profile=profile, stats=stats, global_hypos=global_hypos)

        return AnalysisReport(
            patient_id=patient_id,
            generated_at=datetime.now(UTC),
            period_start=period_start,
            period_end=period_end,
            global_hypo_events=global_hypos,
            warnings=warnings,
            stats=stats,
            recommendations=recommendations,
        )

    def _normalize_dataset(self, dataset: PatientDataset) -> PatientDataset:
        glucose = sorted(dataset.glucose, key=lambda item: item.ts)
        carbs = sorted(dataset.carbs, key=lambda item: item.ts)
        insulin = sorted(dataset.insulin, key=lambda item: item.ts)
        return PatientDataset(glucose=glucose, carbs=carbs, insulin=insulin)

    def _build_stats(self, *, profile: PatientProfile, dataset: PatientDataset) -> tuple[list[BlockStats], int]:
        glucose_by_hour: dict[int, list[GlucosePoint]] = defaultdict(list)
        for point in dataset.glucose:
            glucose_by_hour[point.ts.hour].append(point)

        carbs_by_hour: dict[int, list[CarbEvent]] = defaultdict(list)
        for carb in dataset.carbs:
            carbs_by_hour[carb.ts.hour].append(carb)

        insulin_by_hour: dict[int, list[InsulinEvent]] = defaultdict(list)
        for insulin in dataset.insulin:
            insulin_by_hour[insulin.ts.hour].append(insulin)

        global_hypos = _count_episodes(
            dataset.glucose,
            threshold=self.settings.hypo_threshold_mgdl,
            direction="below",
        )

        stats: list[BlockStats] = []
        for block_settings in profile.blocks:
            block = block_settings.block
            block_glucose = [point for hour, points in glucose_by_hour.items() if block.contains_hour(hour) for point in points]
            block_carbs = [item for hour, items in carbs_by_hour.items() if block.contains_hour(hour) for item in items]
            block_insulin = [item for hour, items in insulin_by_hour.items() if block.contains_hour(hour) for item in items]

            postprandial_deltas = self._postprandial_deltas(block_carbs, dataset.glucose)
            correction_ratios = self._correction_effectiveness_ratios(
                glucose=dataset.glucose,
                insulin=block_insulin,
                carbs=dataset.carbs,
                current_isf=block_settings.isf,
            )
            fasting_drift, fasting_hours, fasting_samples = self._fasting_drift(
                glucose=block_glucose,
                carbs=dataset.carbs,
                insulin=dataset.insulin,
            )

            stats.append(
                BlockStats(
                    block_name=block.name,
                    meals=len(postprandial_deltas),
                    corrections=len(correction_ratios),
                    fasting_hours=fasting_hours,
                    fasting_samples=fasting_samples,
                    hypo_events=_count_episodes(
                        block_glucose,
                        threshold=self.settings.hypo_threshold_mgdl,
                        direction="below",
                    ),
                    hyper_events=_count_episodes(
                        block_glucose,
                        threshold=self.settings.hyper_threshold_mgdl,
                        direction="above",
                    ),
                    mean_postprandial_delta=robust_mean(postprandial_deltas),
                    mean_correction_ratio=robust_mean(correction_ratios),
                    fasting_drift_mgdl_per_hour=fasting_drift,
                    postprandial_variability=_robust_variability(postprandial_deltas),
                    correction_variability=_robust_variability(correction_ratios),
                )
            )

        return stats, global_hypos

    def _build_warnings(self, *, stats: list[BlockStats], global_hypos: int) -> list[str]:
        warnings: list[str] = []
        if global_hypos >= self.settings.global_hypo_guard_limit:
            warnings.append("Высокая частота гипо: агрессивные изменения заблокированы.")

        low_meal_blocks = [stat.block_name for stat in stats if stat.meals < self.settings.min_meals_per_block]
        if low_meal_blocks:
            warnings.append(f"Недостаточно данных по приемам пищи: {', '.join(low_meal_blocks)}.")

        noisy_blocks = [
            stat.block_name
            for stat in stats
            if (
                (stat.postprandial_variability is not None and stat.postprandial_variability > 45)
                or (stat.correction_variability is not None and stat.correction_variability > 0.5)
            )
        ]
        if noisy_blocks:
            warnings.append(f"Повышенная вариативность в блоках: {', '.join(noisy_blocks)}.")
        return warnings

    def _build_recommendations(
        self,
        *,
        profile: PatientProfile,
        stats: list[BlockStats],
        global_hypos: int,
    ) -> list[Recommendation]:
        stat_map = {item.block_name: item for item in stats}
        recommendations: list[Recommendation] = []

        for block in profile.blocks:
            block_stats = stat_map[block.block.name]
            recommendations.extend(
                [
                    self._recommend_icr(block.icr, block.block.name, block_stats, global_hypos),
                    self._recommend_isf(block.isf, block.block.name, block_stats, global_hypos),
                    self._recommend_basal(block.basal, block.block.name, block_stats, global_hypos),
                ]
            )
        return recommendations

    def _recommend_icr(self, current: float, block_name: str, stats: BlockStats, global_hypos: int) -> Recommendation:
        confidence = self._confidence(
            samples=stats.meals,
            min_needed=self.settings.min_meals_per_block,
            variability=stats.postprandial_variability,
        )
        recommendation = Recommendation(
            parameter="icr",
            block_name=block_name,
            current_value=current,
            proposed_value=current,
            percent_change=0.0,
            confidence=confidence,
            rationale=[],
        )

        if stats.meals < self.settings.min_meals_per_block or stats.mean_postprandial_delta is None:
            recommendation.blocked = True
            recommendation.blocked_reason = "Недостаточно данных по приемам пищи в блоке."
            return recommendation

        delta = stats.mean_postprandial_delta
        consistency = self._icr_isf_consistency(stats)
        shift_ratio = min(abs(delta) / 220.0, 0.12)
        shift_ratio = max(0.02, shift_ratio)
        shift_ratio *= consistency

        if delta > 22:
            recommendation.proposed_value = current * (1.0 - shift_ratio)
            recommendation.rationale.append(f"Рост глюкозы после еды: +{delta:.1f} mg/dL.")
        elif delta < -22:
            recommendation.proposed_value = current * (1.0 + shift_ratio)
            recommendation.rationale.append(f"Снижение после еды: {delta:.1f} mg/dL.")
        else:
            recommendation.blocked = True
            recommendation.blocked_reason = "Отклонение после еды в допустимом диапазоне."
            recommendation.rationale.append("Коррекция УК не требуется.")
            return recommendation

        if consistency < 0.8:
            recommendation.rationale.append("Сигналы еды и коррекций не полностью согласованы, шаг уменьшен.")

        return self.safety.apply(recommendation, global_hypos=global_hypos, block_hypos=stats.hypo_events)

    def _recommend_isf(self, current: float, block_name: str, stats: BlockStats, global_hypos: int) -> Recommendation:
        confidence = self._confidence(
            samples=stats.corrections,
            min_needed=self.settings.min_corrections_per_block,
            variability=stats.correction_variability,
            correction_mode=True,
        )
        recommendation = Recommendation(
            parameter="isf",
            block_name=block_name,
            current_value=current,
            proposed_value=current,
            percent_change=0.0,
            confidence=confidence,
            rationale=[],
        )
        if stats.corrections < self.settings.min_corrections_per_block or stats.mean_correction_ratio is None:
            recommendation.blocked = True
            recommendation.blocked_reason = "Недостаточно корректировочных болюсов в блоке."
            return recommendation

        ratio = stats.mean_correction_ratio
        shift_ratio = min(abs(1 - ratio) * 0.6, 0.12)
        shift_ratio = max(0.02, shift_ratio)

        if ratio < 0.88:
            recommendation.proposed_value = current * (1.0 - shift_ratio)
            recommendation.rationale.append(f"Коррекции слабее ожиданий ({ratio:.2f}x).")
        elif ratio > 1.12:
            recommendation.proposed_value = current * (1.0 + shift_ratio)
            recommendation.rationale.append(f"Коррекции сильнее ожиданий ({ratio:.2f}x).")
        else:
            recommendation.blocked = True
            recommendation.blocked_reason = "ФЧИ соответствует наблюдениям."
            recommendation.rationale.append("Коррекция ФЧИ не требуется.")
            return recommendation

        return self.safety.apply(recommendation, global_hypos=global_hypos, block_hypos=stats.hypo_events)

    def _recommend_basal(self, current: float, block_name: str, stats: BlockStats, global_hypos: int) -> Recommendation:
        confidence = self._confidence(
            samples=max(int(stats.fasting_hours), stats.fasting_samples),
            min_needed=self.settings.min_fasting_hours,
            variability=abs(stats.fasting_drift_mgdl_per_hour) if stats.fasting_drift_mgdl_per_hour is not None else None,
        )
        recommendation = Recommendation(
            parameter="basal",
            block_name=block_name,
            current_value=current,
            proposed_value=current,
            percent_change=0.0,
            confidence=confidence,
            rationale=[],
        )

        if stats.fasting_hours < self.settings.min_fasting_hours or stats.fasting_drift_mgdl_per_hour is None:
            recommendation.blocked = True
            recommendation.blocked_reason = "Недостаточно часов голодного окна в блоке."
            return recommendation

        drift = stats.fasting_drift_mgdl_per_hour
        shift_ratio = min(abs(drift) / 120.0, 0.12)
        shift_ratio = max(0.02, shift_ratio)

        if drift > 7:
            recommendation.proposed_value = current * (1.0 + shift_ratio)
            recommendation.rationale.append(f"Рост в голодном окне +{drift:.1f} mg/dL/ч.")
        elif drift < -7:
            recommendation.proposed_value = current * (1.0 - shift_ratio)
            recommendation.rationale.append(f"Снижение в голодном окне {drift:.1f} mg/dL/ч.")
        else:
            recommendation.blocked = True
            recommendation.blocked_reason = "Базальная скорость в пределах целевого тренда."
            recommendation.rationale.append("Коррекция базала не требуется.")
            return recommendation

        return self.safety.apply(recommendation, global_hypos=global_hypos, block_hypos=stats.hypo_events)

    def _confidence(
        self,
        *,
        samples: int,
        min_needed: int,
        variability: float | None,
        correction_mode: bool = False,
    ) -> float:
        if min_needed <= 0:
            base = 1.0
        else:
            base = min(samples / float(min_needed * 2), 1.0)

        if variability is None:
            return base

        if correction_mode:
            penalty = min(max((variability - 0.25) / 1.0, 0.0), 0.5)
        else:
            penalty = min(max((variability - 25.0) / 120.0, 0.0), 0.5)
        return max(0.0, min(1.0, base * (1.0 - penalty)))

    def _icr_isf_consistency(self, stats: BlockStats) -> float:
        if stats.mean_postprandial_delta is None or stats.mean_correction_ratio is None:
            return 1.0
        if stats.mean_postprandial_delta > 20 and stats.mean_correction_ratio < 0.9:
            return 1.0
        if stats.mean_postprandial_delta < -20 and stats.mean_correction_ratio > 1.1:
            return 1.0
        return 0.75

    def _postprandial_deltas(self, carbs: list[CarbEvent], glucose: list[GlucosePoint]) -> list[float]:
        deltas: list[float] = []
        for meal in carbs:
            pre = _nearest_glucose(glucose, meal.ts, timedelta(minutes=20))
            post = _nearest_glucose(glucose, meal.ts + timedelta(hours=2), timedelta(minutes=30))
            if pre and post:
                deltas.append(post.mgdl - pre.mgdl)
        return deltas

    def _correction_effectiveness_ratios(
        self,
        *,
        glucose: list[GlucosePoint],
        insulin: list[InsulinEvent],
        carbs: list[CarbEvent],
        current_isf: float,
    ) -> list[float]:
        ratios: list[float] = []
        for shot in insulin:
            if shot.kind != "bolus":
                continue
            has_nearby_meal = any(abs((meal.ts - shot.ts).total_seconds()) <= 35 * 60 for meal in carbs)
            if has_nearby_meal:
                continue
            pre = _nearest_glucose(glucose, shot.ts, timedelta(minutes=20))
            post = _nearest_glucose(glucose, shot.ts + timedelta(hours=2), timedelta(minutes=30))
            if not pre or not post:
                continue
            observed_drop = pre.mgdl - post.mgdl
            expected_drop = shot.units * current_isf
            if expected_drop <= 0:
                continue
            ratios.append(max(observed_drop / expected_drop, 0))
        return ratios

    def _fasting_drift(
        self,
        *,
        glucose: list[GlucosePoint],
        carbs: list[CarbEvent],
        insulin: list[InsulinEvent],
    ) -> tuple[float | None, float, int]:
        eligible = []
        for point in glucose:
            recent_meal = any(0 <= (point.ts - meal.ts).total_seconds() <= 3 * 3600 for meal in carbs)
            recent_bolus = any(
                event.kind == "bolus" and 0 <= (point.ts - event.ts).total_seconds() <= 3 * 3600 for event in insulin
            )
            if not recent_meal and not recent_bolus:
                eligible.append(point)

        if len(eligible) < 4:
            return None, 0.0, 0

        segments: list[list[GlucosePoint]] = []
        current_segment = [eligible[0]]
        for point in eligible[1:]:
            if (point.ts - current_segment[-1].ts) <= timedelta(minutes=20):
                current_segment.append(point)
            else:
                if len(current_segment) >= 4:
                    segments.append(current_segment)
                current_segment = [point]
        if len(current_segment) >= 4:
            segments.append(current_segment)

        slopes: list[float] = []
        total_hours = 0.0
        for segment in segments:
            hours = (segment[-1].ts - segment[0].ts).total_seconds() / 3600.0
            if hours < 1.5:
                continue
            slope = (segment[-1].mgdl - segment[0].mgdl) / hours
            slopes.append(slope)
            total_hours += hours

        if not slopes:
            return None, 0.0, 0
        return robust_mean(slopes), total_hours, len(slopes)


def _nearest_glucose(points: list[GlucosePoint], target: datetime, tolerance: timedelta) -> GlucosePoint | None:
    best: GlucosePoint | None = None
    best_delta = tolerance + timedelta(seconds=1)
    for point in points:
        delta = abs(point.ts - target)
        if delta <= tolerance and delta < best_delta:
            best = point
            best_delta = delta
    return best


def _robust_variability(values: list[float]) -> float | None:
    if len(values) < 2:
        return None
    robust_mad = mad(values)
    if robust_mad is None:
        return None
    return robust_mad * 1.4826


def _count_episodes(
    points: list[GlucosePoint],
    *,
    threshold: float,
    direction: str,
    min_gap: timedelta = timedelta(minutes=20),
) -> int:
    episodes = 0
    in_episode = False
    last_event_ts: datetime | None = None

    for point in sorted(points, key=lambda item: item.ts):
        if direction == "below":
            in_range = point.mgdl < threshold
        else:
            in_range = point.mgdl > threshold

        if not in_range:
            in_episode = False
            continue

        if not in_episode:
            if last_event_ts is None or (point.ts - last_event_ts) >= min_gap:
                episodes += 1
            in_episode = True
            last_event_ts = point.ts
        else:
            last_event_ts = point.ts
    return episodes
