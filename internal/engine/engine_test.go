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
