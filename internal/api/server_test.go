package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"diatune-safe/internal/config"
	"diatune-safe/internal/service"
)

func TestAPIFlow(t *testing.T) {
	tmp := t.TempDir()
	settings := config.Settings{
		AppAPIKey:              "secret",
		DatabasePath:           filepath.Join(tmp, "api.sqlite3"),
		MinMealsPerBlock:       1,
		MinCorrectionsPerBlock: 1,
		MinFastingHours:        1,
		SafetyMinConfidence:    0,
		GlobalHypoGuardLimit:   99,
		AnalysisLookbackDays:   14,
		MaxDailyChangePct:      4,
		HypoThresholdMgdl:      70,
		HyperThresholdMgdl:     180,
	}
	svc, err := service.New(settings)
	if err != nil {
		t.Fatalf("service init: %v", err)
	}
	defer func() { _ = svc.Close() }()

	server := New(settings, svc)
	h := server.Router()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-API-Key", "secret")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("health status: %d", resp.Code)
	}
	var healthBody map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &healthBody); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if healthBody["version"] == nil || healthBody["version"] == "" {
		t.Fatalf("health version expected")
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/patients/demo/analyze?days=2&prefer_real_data=false", nil)
	req.Header.Set("X-API-Key", "secret")
	resp = httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("analyze status: %d body=%s", resp.Code, resp.Body.String())
	}
	var analyzeBody map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &analyzeBody); err != nil {
		t.Fatalf("decode analyze: %v", err)
	}
	if analyzeBody["run_id"] == nil {
		t.Fatalf("run_id expected")
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/patients/demo/reports/latest", nil)
	req.Header.Set("X-API-Key", "secret")
	resp = httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("latest status: %d", resp.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/patients/demo/backtest?days=14&prefer_real_data=false", nil)
	req.Header.Set("X-API-Key", "secret")
	resp = httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("backtest status: %d body=%s", resp.Code, resp.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/patients/demo/weekly-stats?days=7&prefer_real_data=false", nil)
	req.Header.Set("X-API-Key", "secret")
	resp = httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("weekly-stats status: %d body=%s", resp.Code, resp.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/patients/demo/recommendations/pending", nil)
	req.Header.Set("X-API-Key", "secret")
	resp = httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("pending status: %d", resp.Code)
	}

	_ = os.Remove(settings.DatabasePath)
}
