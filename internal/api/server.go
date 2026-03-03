package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"diatune-safe/internal/config"
	"diatune-safe/internal/domain"
	"diatune-safe/internal/service"
	appversion "diatune-safe/internal/version"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Server struct {
	settings config.Settings
	service  *service.Service
}

type healthResponse struct {
	Status  string `json:"status"`
	Mode    string `json:"mode"`
	Version string `json:"version"`
}

type reportListResponse struct {
	Reports []domain.AnalysisReport `json:"reports"`
}

type recommendationListResponse struct {
	Recommendations []domain.Recommendation `json:"recommendations"`
}

type acknowledgeRequest struct {
	Reviewer string `json:"reviewer"`
}

type acknowledgeResponse struct {
	Acknowledged bool `json:"acknowledged"`
}

func New(settings config.Settings, svc *service.Service) *Server {
	return &Server{settings: settings, service: svc}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(s.authMiddleware)

	r.Get("/healthz", s.handleHealth)
	r.Route("/v1", func(r chi.Router) {
		r.Get("/patients/{patient_id}/profile", s.handleGetProfile)
		r.Put("/patients/{patient_id}/profile", s.handlePutProfile)
		r.Post("/patients/{patient_id}/analyze", s.handleAnalyze)
		r.Get("/patients/{patient_id}/backtest", s.handleBacktest)
		r.Get("/patients/{patient_id}/weekly-stats", s.handleWeeklyStats)
		r.Get("/patients/{patient_id}/reports/latest", s.handleLatestReport)
		r.Get("/patients/{patient_id}/reports", s.handleListReports)
		r.Get("/patients/{patient_id}/recommendations/pending", s.handlePendingRecommendations)
		r.Post("/recommendations/{recommendation_id}/acknowledge", s.handleAcknowledge)
	})
	return r
}

func (s *Server) Run(ctx context.Context, host string, port int) error {
	httpServer := &http.Server{
		Addr:              host + ":" + strconv.Itoa(port),
		Handler:           s.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.settings.AppAPIKey == "" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("X-API-Key") != s.settings.AppAPIKey {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"detail": "Неверный API-ключ"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	mode := "synthetic"
	if s.settings.NightscoutURL != "" {
		mode = "nightscout"
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Mode: mode, Version: appversion.Semver()})
}

func (s *Server) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	patientID := chi.URLParam(r, "patient_id")
	profile, err := s.service.GetProfile(patientID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) handlePutProfile(w http.ResponseWriter, r *http.Request) {
	patientID := chi.URLParam(r, "patient_id")
	var payload domain.PatientProfile
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "Некорректный JSON"})
		return
	}
	profile, err := s.service.SaveProfile(patientID, payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	patientID := chi.URLParam(r, "patient_id")
	days := parseIntDefault(r.URL.Query().Get("days"), 14)
	if days < 1 || days > 90 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "Параметр days должен быть в диапазоне 1..90"})
		return
	}
	preferRealData := parseBoolDefault(r.URL.Query().Get("prefer_real_data"), true)
	report, err := s.service.RunAnalysis(r.Context(), patientID, days, preferRealData)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleLatestReport(w http.ResponseWriter, r *http.Request) {
	patientID := chi.URLParam(r, "patient_id")
	report, err := s.service.GetLatestReport(patientID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	if report == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": "Отчет не найден"})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleBacktest(w http.ResponseWriter, r *http.Request) {
	patientID := chi.URLParam(r, "patient_id")
	days := parseIntDefault(r.URL.Query().Get("days"), 42)
	if days < 7 || days > 180 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "Параметр days должен быть в диапазоне 7..180"})
		return
	}
	preferRealData := parseBoolDefault(r.URL.Query().Get("prefer_real_data"), true)
	report, err := s.service.RunBacktest(r.Context(), patientID, days, preferRealData)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleWeeklyStats(w http.ResponseWriter, r *http.Request) {
	patientID := chi.URLParam(r, "patient_id")
	days := parseIntDefault(r.URL.Query().Get("days"), s.settings.WeeklyStatsLookbackDays)
	if days < 3 || days > 30 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "Параметр days должен быть в диапазоне 3..30"})
		return
	}
	preferRealData := parseBoolDefault(r.URL.Query().Get("prefer_real_data"), true)
	report, err := s.service.GetWeeklyStats(r.Context(), patientID, days, preferRealData)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleListReports(w http.ResponseWriter, r *http.Request) {
	patientID := chi.URLParam(r, "patient_id")
	limit := parseIntDefault(r.URL.Query().Get("limit"), 20)
	if limit < 1 || limit > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "Параметр limit должен быть в диапазоне 1..100"})
		return
	}
	reports, err := s.service.ListReports(patientID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, reportListResponse{Reports: reports})
}

func (s *Server) handlePendingRecommendations(w http.ResponseWriter, r *http.Request) {
	patientID := chi.URLParam(r, "patient_id")
	recs, err := s.service.ListPendingRecommendations(patientID, 100)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, recommendationListResponse{Recommendations: recs})
}

func (s *Server) handleAcknowledge(w http.ResponseWriter, r *http.Request) {
	recID, err := strconv.ParseInt(chi.URLParam(r, "recommendation_id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "Некорректный ID рекомендации"})
		return
	}
	var payload acknowledgeRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "Некорректный JSON"})
		return
	}
	if payload.Reviewer == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "Поле reviewer обязательно"})
		return
	}
	ok, err := s.service.AcknowledgeRecommendation(recID, payload.Reviewer)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": "Рекомендация не найдена"})
		return
	}
	writeJSON(w, http.StatusOK, acknowledgeResponse{Acknowledged: true})
}

func parseIntDefault(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func parseBoolDefault(raw string, fallback bool) bool {
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
