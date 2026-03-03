package service

import (
	"context"
	"math"
	"sort"
	"time"

	"diatune-safe/internal/domain"
)

func (s *Service) RunBacktest(ctx context.Context, patientID string, days int, preferRealData bool) (domain.BacktestReport, error) {
	if days <= 0 {
		days = 42
	}
	if days < 7 {
		days = 7
	}
	if days > 180 {
		days = 180
	}

	periodEnd := time.Now().UTC()
	periodStart := periodEnd.Add(-time.Duration(days) * 24 * time.Hour)

	profile, err := s.GetProfile(patientID)
	if err != nil {
		return domain.BacktestReport{}, err
	}
	dataset, source, err := s.loadDatasetWithSource(ctx, patientID, periodStart, periodEnd, preferRealData)
	if err != nil {
		return domain.BacktestReport{}, err
	}

	daily := []domain.BacktestDayResult{}
	totalRecs := domain.RecommendationStats{}
	qualitySum := 0.0
	qualityCount := 0

	cursor := periodStart
	for cursor.Before(periodEnd) {
		next := cursor.Add(24 * time.Hour)
		if next.After(periodEnd) {
			next = periodEnd
		}

		dayData := sliceDataset(dataset, cursor, next)
		dayMetrics := buildGlycemicMetrics(dayData.Glucose)
		dayAnalysis := s.engine.Analyze(patientID, profile, dayData, cursor, next)
		dayRecs := recommendationStats(dayAnalysis.Recommendations)
		dayQuality := qualityScore(dayMetrics, dayRecs)

		daily = append(daily, domain.BacktestDayResult{
			Date:            cursor.Format("2006-01-02"),
			Metrics:         dayMetrics,
			Recommendations: dayRecs,
			QualityScore:    dayQuality,
		})

		totalRecs = mergeRecommendationStats(totalRecs, dayRecs)
		qualitySum += dayQuality
		qualityCount++
		cursor = next
	}

	avgQuality := 0.0
	if qualityCount > 0 {
		avgQuality = qualitySum / float64(qualityCount)
	}

	return domain.BacktestReport{
		PatientID:              patientID,
		Generated:              time.Now().UTC(),
		DataSource:             source,
		PeriodStart:            periodStart,
		PeriodEnd:              periodEnd,
		Days:                   days,
		OverallMetrics:         buildGlycemicMetrics(dataset.Glucose),
		OverallRecommendations: totalRecs,
		AverageQualityScore:    avgQuality,
		Daily:                  daily,
	}, nil
}

func (s *Service) GetWeeklyStats(ctx context.Context, patientID string, lookbackDays int, preferRealData bool) (domain.WeeklyStatsReport, error) {
	if lookbackDays <= 0 {
		lookbackDays = s.settings.WeeklyStatsLookbackDays
	}
	if lookbackDays <= 0 {
		lookbackDays = 7
	}
	if lookbackDays < 3 {
		lookbackDays = 3
	}
	if lookbackDays > 30 {
		lookbackDays = 30
	}

	currentEnd := time.Now().UTC()
	currentStart := currentEnd.Add(-time.Duration(lookbackDays) * 24 * time.Hour)
	previousEnd := currentStart
	previousStart := previousEnd.Add(-time.Duration(lookbackDays) * 24 * time.Hour)

	profile, err := s.GetProfile(patientID)
	if err != nil {
		return domain.WeeklyStatsReport{}, err
	}

	joined, source, err := s.loadDatasetWithSource(ctx, patientID, previousStart, currentEnd, preferRealData)
	if err != nil {
		return domain.WeeklyStatsReport{}, err
	}

	currentData := sliceDataset(joined, currentStart, currentEnd)
	previousData := sliceDataset(joined, previousStart, previousEnd)
	currentMetrics := buildGlycemicMetrics(currentData.Glucose)
	previousMetrics := buildGlycemicMetrics(previousData.Glucose)

	currentAnalysis := s.engine.Analyze(patientID, profile, currentData, currentStart, currentEnd)
	currentRecs := recommendationStats(currentAnalysis.Recommendations)

	return domain.WeeklyStatsReport{
		PatientID:              patientID,
		Generated:              time.Now().UTC(),
		DataSource:             source,
		LookbackDays:           lookbackDays,
		CurrentStart:           currentStart,
		CurrentEnd:             currentEnd,
		PreviousStart:          previousStart,
		PreviousEnd:            previousEnd,
		CurrentMetrics:         currentMetrics,
		PreviousMetrics:        previousMetrics,
		CurrentRecommendations: currentRecs,
		DeltaTIRPct:            currentMetrics.TimeInRangePct - previousMetrics.TimeInRangePct,
		DeltaBelow70Pct:        currentMetrics.Below70Pct - previousMetrics.Below70Pct,
		DeltaMeanGlucoseMgdl:   currentMetrics.MeanGlucoseMgdl - previousMetrics.MeanGlucoseMgdl,
		DeltaCVPct:             currentMetrics.CVPct - previousMetrics.CVPct,
	}, nil
}

func sliceDataset(ds domain.PatientDataset, from, to time.Time) domain.PatientDataset {
	out := domain.PatientDataset{
		Glucose: make([]domain.GlucosePoint, 0, len(ds.Glucose)),
		Carbs:   make([]domain.CarbEvent, 0, len(ds.Carbs)),
		Insulin: make([]domain.InsulinEvent, 0, len(ds.Insulin)),
	}
	for _, p := range ds.Glucose {
		if !p.TS.Before(from) && p.TS.Before(to) {
			out.Glucose = append(out.Glucose, p)
		}
	}
	for _, p := range ds.Carbs {
		if !p.TS.Before(from) && p.TS.Before(to) {
			out.Carbs = append(out.Carbs, p)
		}
	}
	for _, p := range ds.Insulin {
		if !p.TS.Before(from) && p.TS.Before(to) {
			out.Insulin = append(out.Insulin, p)
		}
	}
	return out
}

func buildGlycemicMetrics(glucose []domain.GlucosePoint) domain.GlycemicMetrics {
	sort.Slice(glucose, func(i, j int) bool { return glucose[i].TS.Before(glucose[j].TS) })
	n := len(glucose)
	if n == 0 {
		return domain.GlycemicMetrics{}
	}

	sum := 0.0
	inRange := 0
	below70 := 0
	below54 := 0
	above180 := 0
	above250 := 0
	for _, p := range glucose {
		v := p.Mgdl
		sum += v
		if v >= 70 && v <= 180 {
			inRange++
		}
		if v < 70 {
			below70++
		}
		if v < 54 {
			below54++
		}
		if v > 180 {
			above180++
		}
		if v > 250 {
			above250++
		}
	}

	mean := sum / float64(n)
	variance := 0.0
	for _, p := range glucose {
		d := p.Mgdl - mean
		variance += d * d
	}
	variance /= float64(n)
	stddev := math.Sqrt(variance)

	cv := 0.0
	if mean > 0 {
		cv = stddev / mean * 100.0
	}

	return domain.GlycemicMetrics{
		Samples:         n,
		MeanGlucoseMgdl: mean,
		StdDevMgdl:      stddev,
		CVPct:           cv,
		GMI:             3.31 + 0.02392*mean,
		TimeInRangePct:  100.0 * float64(inRange) / float64(n),
		Below70Pct:      100.0 * float64(below70) / float64(n),
		Below54Pct:      100.0 * float64(below54) / float64(n),
		Above180Pct:     100.0 * float64(above180) / float64(n),
		Above250Pct:     100.0 * float64(above250) / float64(n),
		HypoEvents:      countEpisodes(glucose, 70, true, 20*time.Minute),
	}
}

func countEpisodes(points []domain.GlucosePoint, threshold float64, below bool, minGap time.Duration) int {
	if len(points) == 0 {
		return 0
	}
	episodes := 0
	inEpisode := false
	var lastSeen time.Time
	for _, p := range points {
		matched := p.Mgdl < threshold
		if !below {
			matched = p.Mgdl > threshold
		}
		if !matched {
			inEpisode = false
			continue
		}
		if !inEpisode {
			if lastSeen.IsZero() || p.TS.Sub(lastSeen) >= minGap {
				episodes++
			}
			inEpisode = true
		}
		lastSeen = p.TS
	}
	return episodes
}

func recommendationStats(recs []domain.Recommendation) domain.RecommendationStats {
	out := domain.RecommendationStats{Total: len(recs)}
	if len(recs) == 0 {
		return out
	}
	sumConf := 0.0
	for _, rec := range recs {
		if rec.Blocked {
			out.Blocked++
		} else {
			out.Open++
		}
		sumConf += rec.Confidence
	}
	out.AvgConfidence = sumConf / float64(len(recs))
	return out
}

func mergeRecommendationStats(a, b domain.RecommendationStats) domain.RecommendationStats {
	total := a.Total + b.Total
	weightedConf := 0.0
	if a.Total > 0 {
		weightedConf += a.AvgConfidence * float64(a.Total)
	}
	if b.Total > 0 {
		weightedConf += b.AvgConfidence * float64(b.Total)
	}

	out := domain.RecommendationStats{
		Total:   total,
		Open:    a.Open + b.Open,
		Blocked: a.Blocked + b.Blocked,
	}
	if total > 0 {
		out.AvgConfidence = weightedConf / float64(total)
	}
	return out
}

func qualityScore(metrics domain.GlycemicMetrics, recs domain.RecommendationStats) float64 {
	tirScore := clamp(metrics.TimeInRangePct/100.0, 0, 1)
	hypoPenalty := clamp(metrics.Below70Pct/6.0, 0, 1)
	actionability := 0.0
	if recs.Total > 0 {
		actionability = float64(recs.Open) / float64(recs.Total)
	}
	confidence := clamp(recs.AvgConfidence, 0, 1)

	score := 0.45*tirScore + 0.25*actionability + 0.20*confidence + 0.10*(1.0-hypoPenalty)
	return clamp(score*100.0, 0, 100)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
