package telegram

import (
	"strings"
	"testing"
	"time"

	"diatune-safe/internal/config"
	"diatune-safe/internal/domain"
)

func TestFormatReportWithSettingsStructuredOutput(t *testing.T) {
	now := time.Date(2026, 3, 3, 19, 0, 0, 0, time.UTC)
	runID := int64(42)
	report := domain.AnalysisReport{
		RunID:            &runID,
		PatientID:        "demo",
		GeneratedAt:      now,
		PeriodStart:      now.Add(-24 * time.Hour),
		PeriodEnd:        now,
		GlobalHypoEvents: 1,
		Warnings:         []string{"Мало данных в блоке 18-22"},
		Recommendations: []domain.Recommendation{
			{
				Parameter:     domain.ParameterICR,
				BlockName:     "06-10",
				CurrentValue:  8.5,
				ProposedValue: 8.9,
				PercentChange: 4.7,
				Confidence:    0.68,
				Rationale:     []string{"Постпрандиальный тренд устойчиво выше цели. Доп. фраза."},
			},
			{
				Parameter:     domain.ParameterBasal,
				BlockName:     "22-02",
				CurrentValue:  0.9,
				ProposedValue: 0.95,
				PercentChange: 5.5,
				Confidence:    0.49,
				Blocked:       true,
				BlockedReason: "вероятность гипо выше порога",
			},
		},
	}

	got := FormatReportWithSettings(report, config.Settings{Timezone: "Europe/Moscow", GlucoseUnit: "mmol"})
	mustContain := []string{
		"Diatune Safe",
		"Сводка:",
		"Предупреждения:",
		"Рекомендации для AAPS (TOP 5):",
		"AAPS:",
		"Обоснование:",
		"Заблокировано (топ причин):",
	}
	for _, part := range mustContain {
		if !strings.Contains(got, part) {
			t.Fatalf("expected %q in output:\n%s", part, got)
		}
	}
}

func TestSplitForTelegram(t *testing.T) {
	text := strings.Repeat("line text\n", 200)
	chunks := SplitForTelegram(text, 600)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks")
	}
	for i, chunk := range chunks {
		if len(strings.TrimSpace(chunk)) == 0 {
			t.Fatalf("chunk %d is empty", i)
		}
		if runeCount := len([]rune(chunk)); runeCount > 600 {
			t.Fatalf("chunk %d too long: %d", i, runeCount)
		}
	}
}
