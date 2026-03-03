package engine

import (
	"testing"
	"time"

	"diatune-safe/internal/config"
	"diatune-safe/internal/domain"
)

func testSettings() config.Settings {
	return config.Settings{
		MinMealsPerBlock:       2,
		MinCorrectionsPerBlock: 2,
		MinFastingHours:        2,
		MaxDailyChangePct:      4,
		SafetyMinConfidence:    0.2,
		GlobalHypoGuardLimit:   20,
		HypoThresholdMgdl:      70,
		HyperThresholdMgdl:     180,
		MonteCarloSamples:      400,
		MinBenefitProbability:  0.58,
		MaxHypoRiskProbability: 0.22,
	}
}

func testProfile() domain.PatientProfile {
	return domain.PatientProfile{
		PatientID: "demo",
		Blocks: []domain.BlockSettings{
			{Block: domain.TimeBlock{Name: "00-23", StartHour: 0, EndHour: 23}, ICR: 10, ISF: 45, Basal: 0.8},
		},
	}
}

func TestAnalyzeProducesRecommendations(t *testing.T) {
	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	glucose := []domain.GlucosePoint{}
	for i := 0; i < 24*12; i++ {
		ts := start.Add(time.Duration(i*5) * time.Minute)
		glucose = append(glucose, domain.GlucosePoint{TS: ts, Mgdl: 110 + float64(i%12)})
	}

	meals := []domain.CarbEvent{
		{TS: start.Add(8 * time.Hour), Grams: 45},
		{TS: start.Add(13 * time.Hour), Grams: 50},
		{TS: start.Add(19 * time.Hour), Grams: 55},
	}
	insulin := []domain.InsulinEvent{
		{TS: start.Add(8*time.Hour - 10*time.Minute), Units: 4.6, Kind: "bolus"},
		{TS: start.Add(13*time.Hour - 10*time.Minute), Units: 4.8, Kind: "bolus"},
		{TS: start.Add(19*time.Hour - 10*time.Minute), Units: 5.4, Kind: "bolus"},
		{TS: start.Add(4 * time.Hour), Units: 1.2, Kind: "bolus"},
		{TS: start.Add(16 * time.Hour), Units: 1.0, Kind: "bolus"},
	}

	dataset := domain.PatientDataset{Glucose: glucose, Carbs: meals, Insulin: insulin}
	eng := New(testSettings())
	report := eng.Analyze("demo", testProfile(), dataset, start, start.Add(24*time.Hour))

	if len(report.Recommendations) != 3 {
		t.Fatalf("expected 3 recommendations, got %d", len(report.Recommendations))
	}
}

func TestRecommendICRWithStrongSignal(t *testing.T) {
	eng := New(testSettings())
	block := domain.BlockSettings{
		Block: domain.TimeBlock{Name: "08-11", StartHour: 8, EndHour: 11},
		ICR:   10,
		ISF:   45,
		Basal: 0.8,
	}
	delta := 60.0
	variability := 14.0
	mealCarbs := 48.0
	ratio := 0.82
	st := domain.BlockStats{
		BlockName:               "08-11",
		Meals:                   8,
		Corrections:             6,
		HypoEvents:              0,
		MeanPostprandialDelta:   &delta,
		PostprandialVariability: &variability,
		AvgMealCarbs:            &mealCarbs,
		MeanCorrectionRatio:     &ratio,
	}

	rec := eng.recommendICR(block, st, 0)
	if rec.Blocked {
		t.Fatalf("expected non-blocked recommendation, reason=%s", rec.BlockedReason)
	}
	if rec.ProposedValue >= block.ICR {
		t.Fatalf("expected ICR to decrease, got %.3f", rec.ProposedValue)
	}
	if rec.Confidence <= 0.5 {
		t.Fatalf("expected confidence > 0.5, got %.3f", rec.Confidence)
	}
}

func TestRecommendICRHighBenefitThresholdLowersConfidence(t *testing.T) {
	s := testSettings()
	s.MinBenefitProbability = 0.98
	eng := New(s)
	block := domain.BlockSettings{
		Block: domain.TimeBlock{Name: "08-11", StartHour: 8, EndHour: 11},
		ICR:   10,
		ISF:   45,
		Basal: 0.8,
	}
	delta := 24.0
	variability := 45.0
	mealCarbs := 40.0
	ratio := 0.95
	st := domain.BlockStats{
		BlockName:               "08-11",
		Meals:                   3,
		Corrections:             2,
		HypoEvents:              0,
		MeanPostprandialDelta:   &delta,
		PostprandialVariability: &variability,
		AvgMealCarbs:            &mealCarbs,
		MeanCorrectionRatio:     &ratio,
	}

	rec := eng.recommendICR(block, st, 0)
	if rec.Confidence >= 0.7 {
		t.Fatalf("expected confidence penalty due strict probability thresholds, got %.3f", rec.Confidence)
	}
}
