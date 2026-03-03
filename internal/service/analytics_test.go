package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"diatune-safe/internal/config"
	"diatune-safe/internal/domain"
)

func testServiceSettings(dbPath string) config.Settings {
	return config.Settings{
		DatabasePath:            dbPath,
		Timezone:                "UTC",
		AnalysisLookbackDays:    14,
		MinMealsPerBlock:        1,
		MinCorrectionsPerBlock:  1,
		MinFastingHours:         1,
		MaxDailyChangePct:       4,
		SafetyMinConfidence:     0,
		GlobalHypoGuardLimit:    99,
		HypoThresholdMgdl:       70,
		HyperThresholdMgdl:      180,
		MonteCarloSamples:       200,
		MinBenefitProbability:   0,
		MaxHypoRiskProbability:  1,
		WeeklyStatsLookbackDays: 7,
	}
}

func TestBuildGlycemicMetrics(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	points := []domain.GlucosePoint{
		{TS: start, Mgdl: 65},
		{TS: start.Add(5 * time.Minute), Mgdl: 72},
		{TS: start.Add(10 * time.Minute), Mgdl: 181},
		{TS: start.Add(15 * time.Minute), Mgdl: 250},
	}

	m := buildGlycemicMetrics(points)
	if m.Samples != 4 {
		t.Fatalf("samples: expected 4, got %d", m.Samples)
	}
	if m.Below70Pct <= 0 || m.TimeInRangePct <= 0 || m.Above180Pct <= 0 {
		t.Fatalf("expected non-zero percentages, got %+v", m)
	}
}

func TestRunBacktestAndWeeklyStats(t *testing.T) {
	tmp := t.TempDir()
	settings := testServiceSettings(filepath.Join(tmp, "svc.sqlite3"))
	svc, err := New(settings)
	if err != nil {
		t.Fatalf("service init: %v", err)
	}
	defer func() { _ = svc.Close() }()

	bt, err := svc.RunBacktest(context.Background(), "demo", 10, false)
	if err != nil {
		t.Fatalf("run backtest: %v", err)
	}
	if bt.Days != 10 {
		t.Fatalf("expected 10 days, got %d", bt.Days)
	}
	if len(bt.Daily) == 0 {
		t.Fatalf("expected non-empty daily results")
	}

	ws, err := svc.GetWeeklyStats(context.Background(), "demo", 7, false)
	if err != nil {
		t.Fatalf("weekly stats: %v", err)
	}
	if ws.LookbackDays != 7 {
		t.Fatalf("expected lookback 7, got %d", ws.LookbackDays)
	}
	if ws.CurrentMetrics.Samples == 0 {
		t.Fatalf("expected current metrics with samples")
	}
}
