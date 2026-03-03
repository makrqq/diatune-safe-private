package safety

import (
	"testing"

	"diatune-safe/internal/config"
	"diatune-safe/internal/domain"
)

func testSettings() config.Settings {
	return config.Settings{
		MaxDailyChangePct:    4.0,
		SafetyMinConfidence:  0.55,
		GlobalHypoGuardLimit: 2,
	}
}

func TestClampChange(t *testing.T) {
	p := New(testSettings())
	rec := domain.Recommendation{
		Parameter:     domain.ParameterICR,
		BlockName:     "08-11",
		CurrentValue:  10,
		ProposedValue: 7,
		PercentChange: -30,
		Confidence:    0.95,
		Rationale:     []string{"test"},
	}
	out := p.Apply(rec, 0, 0)
	if out.ProposedValue != 9.6 {
		t.Fatalf("expected 9.6, got %v", out.ProposedValue)
	}
	if out.Blocked {
		t.Fatalf("expected unblocked")
	}
}

func TestBlockAggressiveOnHypos(t *testing.T) {
	p := New(testSettings())
	rec := domain.Recommendation{
		Parameter:     domain.ParameterBasal,
		BlockName:     "00-03",
		CurrentValue:  0.7,
		ProposedValue: 0.9,
		Confidence:    0.9,
	}
	out := p.Apply(rec, 0, 3)
	if !out.Blocked {
		t.Fatalf("expected blocked")
	}
}
