package engine

import (
	"math"
	"sort"
	"strconv"
	"time"

	"diatune-safe/internal/config"
	"diatune-safe/internal/domain"
	"diatune-safe/internal/safety"
	"diatune-safe/internal/stats"
)

type Engine struct {
	settings config.Settings
	safety   safety.Policy
}

func New(settings config.Settings) Engine {
	return Engine{
		settings: settings,
		safety:   safety.New(settings),
	}
}

func (e Engine) Analyze(patientID string, profile domain.PatientProfile, dataset domain.PatientDataset, periodStart, periodEnd time.Time) domain.AnalysisReport {
	norm := normalizeDataset(dataset)
	blockStats, globalHypos := e.buildStats(profile, norm)
	warnings := e.buildWarnings(blockStats, globalHypos)
	recs := e.buildRecommendations(profile, blockStats, globalHypos)

	return domain.AnalysisReport{
		PatientID:        patientID,
		GeneratedAt:      time.Now().UTC(),
		PeriodStart:      periodStart,
		PeriodEnd:        periodEnd,
		GlobalHypoEvents: globalHypos,
		Warnings:         warnings,
		Stats:            blockStats,
		Recommendations:  recs,
	}
}

func normalizeDataset(d domain.PatientDataset) domain.PatientDataset {
	sort.Slice(d.Glucose, func(i, j int) bool { return d.Glucose[i].TS.Before(d.Glucose[j].TS) })
	sort.Slice(d.Carbs, func(i, j int) bool { return d.Carbs[i].TS.Before(d.Carbs[j].TS) })
	sort.Slice(d.Insulin, func(i, j int) bool { return d.Insulin[i].TS.Before(d.Insulin[j].TS) })
	return d
}

func (e Engine) buildStats(profile domain.PatientProfile, dataset domain.PatientDataset) ([]domain.BlockStats, int) {
	globalHypos := countEpisodes(dataset.Glucose, float64(e.settings.HypoThresholdMgdl), "below", 20*time.Minute)
	statsList := make([]domain.BlockStats, 0, len(profile.Blocks))

	for _, blockSettings := range profile.Blocks {
		block := blockSettings.Block
		blockGlucose := filterGlucoseByBlock(dataset.Glucose, block)
		blockCarbs := filterCarbsByBlock(dataset.Carbs, block)
		blockInsulin := filterInsulinByBlock(dataset.Insulin, block)

		postprandial := e.postprandialDeltas(blockCarbs, dataset.Glucose)
		correctionRatios := e.correctionEffectivenessRatios(dataset.Glucose, blockInsulin, dataset.Carbs, blockSettings.ISF)
		fastingDrift, fastingHours, fastingSamples := e.fastingDrift(blockGlucose, dataset.Carbs, dataset.Insulin)

		statsList = append(statsList, domain.BlockStats{
			BlockName:               block.Name,
			Meals:                   len(postprandial),
			Corrections:             len(correctionRatios),
			FastingHours:            fastingHours,
			FastingSamples:          fastingSamples,
			HypoEvents:              countEpisodes(blockGlucose, float64(e.settings.HypoThresholdMgdl), "below", 20*time.Minute),
			HyperEvents:             countEpisodes(blockGlucose, float64(e.settings.HyperThresholdMgdl), "above", 20*time.Minute),
			MeanPostprandialDelta:   stats.RobustMean(postprandial),
			MeanCorrectionRatio:     stats.RobustMean(correctionRatios),
			FastingDriftMgdlPerHour: fastingDrift,
			PostprandialVariability: robustVariability(postprandial),
			CorrectionVariability:   robustVariability(correctionRatios),
		})
	}

	return statsList, globalHypos
}

func (e Engine) buildWarnings(blockStats []domain.BlockStats, globalHypos int) []string {
	warnings := []string{}
	if globalHypos >= e.settings.GlobalHypoGuardLimit {
		warnings = append(warnings, "Высокая частота гипо: агрессивные изменения заблокированы.")
	}

	lowMealBlocks := []string{}
	for _, s := range blockStats {
		if s.Meals < e.settings.MinMealsPerBlock {
			lowMealBlocks = append(lowMealBlocks, s.BlockName)
		}
	}
	if len(lowMealBlocks) > 0 {
		warnings = append(warnings, "Недостаточно данных по приемам пищи: "+joinComma(lowMealBlocks)+".")
	}

	noisyBlocks := []string{}
	for _, s := range blockStats {
		if (s.PostprandialVariability != nil && *s.PostprandialVariability > 45) ||
			(s.CorrectionVariability != nil && *s.CorrectionVariability > 0.5) {
			noisyBlocks = append(noisyBlocks, s.BlockName)
		}
	}
	if len(noisyBlocks) > 0 {
		warnings = append(warnings, "Повышенная вариативность в блоках: "+joinComma(noisyBlocks)+".")
	}
	return warnings
}

func (e Engine) buildRecommendations(profile domain.PatientProfile, statsList []domain.BlockStats, globalHypos int) []domain.Recommendation {
	byBlock := map[string]domain.BlockStats{}
	for _, s := range statsList {
		byBlock[s.BlockName] = s
	}

	out := make([]domain.Recommendation, 0, len(profile.Blocks)*3)
	for _, block := range profile.Blocks {
		s := byBlock[block.Block.Name]
		out = append(out,
			e.recommendICR(block.ICR, block.Block.Name, s, globalHypos),
			e.recommendISF(block.ISF, block.Block.Name, s, globalHypos),
			e.recommendBasal(block.Basal, block.Block.Name, s, globalHypos),
		)
	}
	return out
}

func (e Engine) recommendICR(current float64, blockName string, st domain.BlockStats, globalHypos int) domain.Recommendation {
	conf := e.confidence(st.Meals, e.settings.MinMealsPerBlock, st.PostprandialVariability, false)
	rec := domain.Recommendation{
		Parameter:     domain.ParameterICR,
		BlockName:     blockName,
		CurrentValue:  current,
		ProposedValue: current,
		PercentChange: 0,
		Confidence:    conf,
		Rationale:     []string{},
	}
	if st.Meals < e.settings.MinMealsPerBlock || st.MeanPostprandialDelta == nil {
		rec.Blocked = true
		rec.BlockedReason = "Недостаточно данных по приемам пищи в блоке."
		return rec
	}
	delta := *st.MeanPostprandialDelta
	consistency := icrIsfConsistency(st)
	shift := math.Min(math.Abs(delta)/220.0, 0.12)
	if shift < 0.02 {
		shift = 0.02
	}
	shift *= consistency

	if delta > 22 {
		rec.ProposedValue = current * (1.0 - shift)
		rec.Rationale = append(rec.Rationale, "Рост глюкозы после еды: "+formatSigned(delta)+" mg/dL.")
	} else if delta < -22 {
		rec.ProposedValue = current * (1.0 + shift)
		rec.Rationale = append(rec.Rationale, "Снижение после еды: "+formatSigned(delta)+" mg/dL.")
	} else {
		rec.Blocked = true
		rec.BlockedReason = "Отклонение после еды в допустимом диапазоне."
		rec.Rationale = append(rec.Rationale, "Коррекция УК не требуется.")
		return rec
	}
	if consistency < 0.8 {
		rec.Rationale = append(rec.Rationale, "Сигналы еды и коррекций не полностью согласованы, шаг уменьшен.")
	}
	return e.safety.Apply(rec, globalHypos, st.HypoEvents)
}

func (e Engine) recommendISF(current float64, blockName string, st domain.BlockStats, globalHypos int) domain.Recommendation {
	conf := e.confidence(st.Corrections, e.settings.MinCorrectionsPerBlock, st.CorrectionVariability, true)
	rec := domain.Recommendation{
		Parameter:     domain.ParameterISF,
		BlockName:     blockName,
		CurrentValue:  current,
		ProposedValue: current,
		PercentChange: 0,
		Confidence:    conf,
		Rationale:     []string{},
	}
	if st.Corrections < e.settings.MinCorrectionsPerBlock || st.MeanCorrectionRatio == nil {
		rec.Blocked = true
		rec.BlockedReason = "Недостаточно корректировочных болюсов в блоке."
		return rec
	}
	ratio := *st.MeanCorrectionRatio
	shift := math.Min(math.Abs(1-ratio)*0.6, 0.12)
	if shift < 0.02 {
		shift = 0.02
	}
	if ratio < 0.88 {
		rec.ProposedValue = current * (1.0 - shift)
		rec.Rationale = append(rec.Rationale, "Коррекции слабее ожиданий ("+formatFloat(ratio, 2)+"x).")
	} else if ratio > 1.12 {
		rec.ProposedValue = current * (1.0 + shift)
		rec.Rationale = append(rec.Rationale, "Коррекции сильнее ожиданий ("+formatFloat(ratio, 2)+"x).")
	} else {
		rec.Blocked = true
		rec.BlockedReason = "ФЧИ соответствует наблюдениям."
		rec.Rationale = append(rec.Rationale, "Коррекция ФЧИ не требуется.")
		return rec
	}
	return e.safety.Apply(rec, globalHypos, st.HypoEvents)
}

func (e Engine) recommendBasal(current float64, blockName string, st domain.BlockStats, globalHypos int) domain.Recommendation {
	var variability *float64
	if st.FastingDriftMgdlPerHour != nil {
		v := math.Abs(*st.FastingDriftMgdlPerHour)
		variability = &v
	}
	samples := int(st.FastingHours)
	if st.FastingSamples > samples {
		samples = st.FastingSamples
	}
	conf := e.confidence(samples, e.settings.MinFastingHours, variability, false)
	rec := domain.Recommendation{
		Parameter:     domain.ParameterBasal,
		BlockName:     blockName,
		CurrentValue:  current,
		ProposedValue: current,
		PercentChange: 0,
		Confidence:    conf,
		Rationale:     []string{},
	}
	if st.FastingHours < float64(e.settings.MinFastingHours) || st.FastingDriftMgdlPerHour == nil {
		rec.Blocked = true
		rec.BlockedReason = "Недостаточно часов голодного окна в блоке."
		return rec
	}
	drift := *st.FastingDriftMgdlPerHour
	shift := math.Min(math.Abs(drift)/120.0, 0.12)
	if shift < 0.02 {
		shift = 0.02
	}
	if drift > 7 {
		rec.ProposedValue = current * (1.0 + shift)
		rec.Rationale = append(rec.Rationale, "Рост в голодном окне "+formatSigned(drift)+" mg/dL/ч.")
	} else if drift < -7 {
		rec.ProposedValue = current * (1.0 - shift)
		rec.Rationale = append(rec.Rationale, "Снижение в голодном окне "+formatSigned(drift)+" mg/dL/ч.")
	} else {
		rec.Blocked = true
		rec.BlockedReason = "Базальная скорость в пределах целевого тренда."
		rec.Rationale = append(rec.Rationale, "Коррекция базала не требуется.")
		return rec
	}
	return e.safety.Apply(rec, globalHypos, st.HypoEvents)
}

func (e Engine) confidence(samples, minNeeded int, variability *float64, correctionMode bool) float64 {
	base := 1.0
	if minNeeded > 0 {
		base = math.Min(float64(samples)/float64(minNeeded*2), 1.0)
	}
	if variability == nil {
		return base
	}
	penalty := 0.0
	if correctionMode {
		penalty = clamp(((*variability)-0.25)/1.0, 0, 0.5)
	} else {
		penalty = clamp(((*variability)-25.0)/120.0, 0, 0.5)
	}
	return clamp(base*(1.0-penalty), 0, 1)
}

func icrIsfConsistency(st domain.BlockStats) float64 {
	if st.MeanPostprandialDelta == nil || st.MeanCorrectionRatio == nil {
		return 1.0
	}
	if *st.MeanPostprandialDelta > 20 && *st.MeanCorrectionRatio < 0.9 {
		return 1.0
	}
	if *st.MeanPostprandialDelta < -20 && *st.MeanCorrectionRatio > 1.1 {
		return 1.0
	}
	return 0.75
}

func (e Engine) postprandialDeltas(carbs []domain.CarbEvent, glucose []domain.GlucosePoint) []float64 {
	out := []float64{}
	for _, meal := range carbs {
		pre := nearestGlucose(glucose, meal.TS, 20*time.Minute)
		post := nearestGlucose(glucose, meal.TS.Add(2*time.Hour), 30*time.Minute)
		if pre != nil && post != nil {
			out = append(out, post.Mgdl-pre.Mgdl)
		}
	}
	return out
}

func (e Engine) correctionEffectivenessRatios(glucose []domain.GlucosePoint, insulin []domain.InsulinEvent, carbs []domain.CarbEvent, currentISF float64) []float64 {
	out := []float64{}
	for _, shot := range insulin {
		if shot.Kind != "bolus" {
			continue
		}
		hasNearbyMeal := false
		for _, meal := range carbs {
			d := meal.TS.Sub(shot.TS)
			if d < 0 {
				d = -d
			}
			if d <= 35*time.Minute {
				hasNearbyMeal = true
				break
			}
		}
		if hasNearbyMeal {
			continue
		}
		pre := nearestGlucose(glucose, shot.TS, 20*time.Minute)
		post := nearestGlucose(glucose, shot.TS.Add(2*time.Hour), 30*time.Minute)
		if pre == nil || post == nil {
			continue
		}
		observed := pre.Mgdl - post.Mgdl
		expected := shot.Units * currentISF
		if expected <= 0 {
			continue
		}
		r := observed / expected
		if r < 0 {
			r = 0
		}
		out = append(out, r)
	}
	return out
}

func (e Engine) fastingDrift(glucose []domain.GlucosePoint, carbs []domain.CarbEvent, insulin []domain.InsulinEvent) (*float64, float64, int) {
	eligible := []domain.GlucosePoint{}
	for _, p := range glucose {
		recentMeal := false
		for _, meal := range carbs {
			d := p.TS.Sub(meal.TS)
			if d >= 0 && d <= 3*time.Hour {
				recentMeal = true
				break
			}
		}
		recentBolus := false
		for _, shot := range insulin {
			if shot.Kind != "bolus" {
				continue
			}
			d := p.TS.Sub(shot.TS)
			if d >= 0 && d <= 3*time.Hour {
				recentBolus = true
				break
			}
		}
		if !recentMeal && !recentBolus {
			eligible = append(eligible, p)
		}
	}
	if len(eligible) < 4 {
		return nil, 0, 0
	}

	segments := [][]domain.GlucosePoint{{eligible[0]}}
	for _, p := range eligible[1:] {
		cur := segments[len(segments)-1]
		if p.TS.Sub(cur[len(cur)-1].TS) <= 20*time.Minute {
			segments[len(segments)-1] = append(cur, p)
		} else {
			segments = append(segments, []domain.GlucosePoint{p})
		}
	}

	slopes := []float64{}
	totalHours := 0.0
	for _, seg := range segments {
		if len(seg) < 4 {
			continue
		}
		hours := seg[len(seg)-1].TS.Sub(seg[0].TS).Hours()
		if hours < 1.5 {
			continue
		}
		slope := (seg[len(seg)-1].Mgdl - seg[0].Mgdl) / hours
		slopes = append(slopes, slope)
		totalHours += hours
	}
	if len(slopes) == 0 {
		return nil, 0, 0
	}
	return stats.RobustMean(slopes), totalHours, len(slopes)
}

func filterGlucoseByBlock(glucose []domain.GlucosePoint, block domain.TimeBlock) []domain.GlucosePoint {
	out := []domain.GlucosePoint{}
	for _, p := range glucose {
		if block.ContainsHour(p.TS.Hour()) {
			out = append(out, p)
		}
	}
	return out
}

func filterCarbsByBlock(carbs []domain.CarbEvent, block domain.TimeBlock) []domain.CarbEvent {
	out := []domain.CarbEvent{}
	for _, p := range carbs {
		if block.ContainsHour(p.TS.Hour()) {
			out = append(out, p)
		}
	}
	return out
}

func filterInsulinByBlock(insulin []domain.InsulinEvent, block domain.TimeBlock) []domain.InsulinEvent {
	out := []domain.InsulinEvent{}
	for _, p := range insulin {
		if block.ContainsHour(p.TS.Hour()) {
			out = append(out, p)
		}
	}
	return out
}

func nearestGlucose(points []domain.GlucosePoint, target time.Time, tolerance time.Duration) *domain.GlucosePoint {
	var best *domain.GlucosePoint
	bestDelta := tolerance + time.Second
	for i := range points {
		d := points[i].TS.Sub(target)
		if d < 0 {
			d = -d
		}
		if d <= tolerance && d < bestDelta {
			bestDelta = d
			best = &points[i]
		}
	}
	return best
}

func robustVariability(values []float64) *float64 {
	if len(values) < 2 {
		return nil
	}
	mad := stats.MAD(values)
	if mad == nil {
		return nil
	}
	v := (*mad) * 1.4826
	return &v
}

func countEpisodes(points []domain.GlucosePoint, threshold float64, direction string, minGap time.Duration) int {
	sort.Slice(points, func(i, j int) bool { return points[i].TS.Before(points[j].TS) })
	episodes := 0
	inEpisode := false
	var lastEvent time.Time

	for _, p := range points {
		inRange := false
		if direction == "below" {
			inRange = p.Mgdl < threshold
		} else {
			inRange = p.Mgdl > threshold
		}

		if !inRange {
			inEpisode = false
			continue
		}
		if !inEpisode {
			if lastEvent.IsZero() || p.TS.Sub(lastEvent) >= minGap {
				episodes++
			}
			inEpisode = true
			lastEvent = p.TS
		} else {
			lastEvent = p.TS
		}
	}
	return episodes
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

func joinComma(items []string) string {
	if len(items) == 0 {
		return ""
	}
	out := items[0]
	for _, item := range items[1:] {
		out += ", " + item
	}
	return out
}

func formatSigned(v float64) string {
	if v >= 0 {
		return "+" + formatFloat(v, 1)
	}
	return formatFloat(v, 1)
}

func formatFloat(v float64, decimals int) string {
	pow := math.Pow10(decimals)
	r := math.Round(v*pow) / pow
	return strconvFormat(r, decimals)
}

func strconvFormat(v float64, decimals int) string {
	return strconv.FormatFloat(v, 'f', decimals, 64)
}
