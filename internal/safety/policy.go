package safety

import (
	"math"

	"diatune-safe/internal/config"
	"diatune-safe/internal/domain"
)

type ranges struct {
	icrMin   float64
	icrMax   float64
	isfMin   float64
	isfMax   float64
	basalMin float64
	basalMax float64
}

type Policy struct {
	settings config.Settings
	ranges   ranges
}

func New(settings config.Settings) Policy {
	return Policy{
		settings: settings,
		ranges: ranges{
			icrMin:   2,
			icrMax:   40,
			isfMin:   10,
			isfMax:   300,
			basalMin: 0.05,
			basalMax: 5,
		},
	}
}

func (p Policy) Apply(rec domain.Recommendation, globalHypos int, blockHypos int) domain.Recommendation {
	if rec.Confidence < p.settings.SafetyMinConfidence {
		rec.Blocked = true
		rec.BlockedReason = "Низкая уверенность рекомендации."
		rec.Rationale = append(rec.Rationale, "Требуется больше данных для безопасной корректировки.")
		return rec
	}

	if globalHypos >= p.settings.GlobalHypoGuardLimit {
		rec.Blocked = true
		rec.BlockedReason = "Слишком много гипо за период анализа."
		rec.Rationale = append(rec.Rationale, "Алгоритм блокирует все ужесточающие изменения.")
		return rec
	}

	if blockHypos > 0 && p.isAggressive(rec) {
		rec.Blocked = true
		rec.BlockedReason = "Обнаружены гипо в блоке; ужесточение запрещено."
		rec.Rationale = append(rec.Rationale, "Допустимы только более мягкие изменения после ручного ревью.")
		return rec
	}

	rec.ProposedValue = p.clampByParameter(rec.Parameter, rec.ProposedValue)
	rec.ProposedValue = p.clampChange(rec.CurrentValue, rec.ProposedValue, p.settings.MaxDailyChangePct)
	rec.PercentChange = 100.0 * (rec.ProposedValue - rec.CurrentValue) / rec.CurrentValue

	if math.Abs(rec.PercentChange) < 0.5 {
		rec.Blocked = true
		rec.BlockedReason = "Изменение слишком мало для практической пользы."
	}
	return rec
}

func (p Policy) isAggressive(rec domain.Recommendation) bool {
	switch rec.Parameter {
	case domain.ParameterICR, domain.ParameterISF:
		return rec.ProposedValue < rec.CurrentValue
	case domain.ParameterBasal:
		return rec.ProposedValue > rec.CurrentValue
	default:
		return false
	}
}

func (p Policy) clampChange(current float64, proposed float64, maxPct float64) float64 {
	delta := current * (maxPct / 100.0)
	lower := current - delta
	upper := current + delta
	if proposed < lower {
		return lower
	}
	if proposed > upper {
		return upper
	}
	return proposed
}

func (p Policy) clampByParameter(param domain.ParameterName, value float64) float64 {
	switch param {
	case domain.ParameterICR:
		return clamp(value, p.ranges.icrMin, p.ranges.icrMax)
	case domain.ParameterISF:
		return clamp(value, p.ranges.isfMin, p.ranges.isfMax)
	case domain.ParameterBasal:
		return clamp(value, p.ranges.basalMin, p.ranges.basalMax)
	default:
		return value
	}
}

func clamp(v, low, high float64) float64 {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}
