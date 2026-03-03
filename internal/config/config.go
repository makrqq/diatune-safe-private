package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Settings struct {
	AppEnv       string
	AppHost      string
	AppPort      int
	AppAPIKey    string
	DatabasePath string
	Timezone     string
	LogLevel     string
	Locale       string
	GlucoseUnit  string

	TelegramBotToken       string
	TelegramAllowedUserIDs []int64

	NightscoutURL       string
	NightscoutAPISecret string

	AnalysisLookbackDays   int
	MaxDailyChangePct      float64
	MinMealsPerBlock       int
	MinCorrectionsPerBlock int
	MinFastingHours        int
	HypoThresholdMgdl      int
	HyperThresholdMgdl     int
	GlobalHypoGuardLimit   int
	SafetyMinConfidence    float64
	MonteCarloSamples      int
	MinBenefitProbability  float64
	MaxHypoRiskProbability float64

	AutoAnalysisEnabled         bool
	AutoAnalysisIntervalMinutes int
	AutoAnalysisPatientIDs      []string

	DailyRecommendationEnabled    bool
	DailyRecommendationTime       string
	DailyRecommendationPatientIDs []string

	WeeklyStatsEnabled      bool
	WeeklyStatsDay          string
	WeeklyStatsTime         string
	WeeklyStatsLookbackDays int
	WeeklyStatsPatientIDs   []string
}

func Load() (Settings, error) {
	_ = godotenv.Load()

	s := Settings{
		AppEnv:       envString("APP_ENV", "development"),
		AppHost:      envString("APP_HOST", "0.0.0.0"),
		AppPort:      envInt("APP_PORT", 8080),
		AppAPIKey:    envString("APP_API_KEY", ""),
		DatabasePath: envString("DATABASE_PATH", "/workspace/data/diatune_safe.sqlite3"),
		Timezone:     envString("TIMEZONE", "Europe/Moscow"),
		LogLevel:     strings.ToUpper(envString("LOG_LEVEL", "INFO")),
		Locale:       envString("LOCALE", "ru-RU"),
		GlucoseUnit:  strings.ToLower(envString("GLUCOSE_UNIT", "mmol")),

		TelegramBotToken:       envString("TELEGRAM_BOT_TOKEN", ""),
		TelegramAllowedUserIDs: envInt64List("TELEGRAM_ALLOWED_USER_IDS"),

		NightscoutURL:       strings.TrimRight(envString("NIGHTSCOUT_URL", ""), "/"),
		NightscoutAPISecret: envString("NIGHTSCOUT_API_SECRET", ""),

		AnalysisLookbackDays:   envInt("ANALYSIS_LOOKBACK_DAYS", 14),
		MaxDailyChangePct:      envFloat("MAX_DAILY_CHANGE_PCT", 8.0),
		MinMealsPerBlock:       envInt("MIN_MEALS_PER_BLOCK", 1),
		MinCorrectionsPerBlock: envInt("MIN_CORRECTIONS_PER_BLOCK", 1),
		MinFastingHours:        envInt("MIN_FASTING_HOURS", 2),
		HypoThresholdMgdl:      envInt("HYPO_THRESHOLD_MGDL", 70),
		HyperThresholdMgdl:     envInt("HYPER_THRESHOLD_MGDL", 180),
		GlobalHypoGuardLimit:   envInt("GLOBAL_HYPO_GUARD_LIMIT", 30),
		SafetyMinConfidence:    envFloat("SAFETY_MIN_CONFIDENCE", 0.08),
		MonteCarloSamples:      envInt("MONTE_CARLO_SAMPLES", 1200),
		MinBenefitProbability:  envFloat("MIN_BENEFIT_PROBABILITY", 0.35),
		MaxHypoRiskProbability: envFloat("MAX_HYPO_RISK_PROBABILITY", 0.60),

		AutoAnalysisEnabled:         envBool("AUTO_ANALYSIS_ENABLED", false),
		AutoAnalysisIntervalMinutes: envInt("AUTO_ANALYSIS_INTERVAL_MINUTES", 360),
		AutoAnalysisPatientIDs:      envStringList("AUTO_ANALYSIS_PATIENT_IDS"),

		DailyRecommendationEnabled:    envBool("DAILY_RECOMMENDATION_ENABLED", false),
		DailyRecommendationTime:       envString("DAILY_RECOMMENDATION_TIME", "22:00"),
		DailyRecommendationPatientIDs: envStringList("DAILY_RECOMMENDATION_PATIENT_IDS"),

		WeeklyStatsEnabled:      envBool("WEEKLY_STATS_ENABLED", false),
		WeeklyStatsDay:          strings.ToLower(envString("WEEKLY_STATS_DAY", "mon")),
		WeeklyStatsTime:         envString("WEEKLY_STATS_TIME", "21:00"),
		WeeklyStatsLookbackDays: envInt("WEEKLY_STATS_LOOKBACK_DAYS", 7),
		WeeklyStatsPatientIDs:   envStringList("WEEKLY_STATS_PATIENT_IDS"),
	}

	if s.AppPort <= 0 {
		return Settings{}, fmt.Errorf("APP_PORT must be positive")
	}
	if s.AnalysisLookbackDays <= 0 {
		return Settings{}, fmt.Errorf("ANALYSIS_LOOKBACK_DAYS must be positive")
	}
	if s.MonteCarloSamples < 200 {
		s.MonteCarloSamples = 200
	}
	if s.MonteCarloSamples > 5000 {
		s.MonteCarloSamples = 5000
	}
	if s.MinBenefitProbability < 0 || s.MinBenefitProbability > 1 {
		return Settings{}, fmt.Errorf("MIN_BENEFIT_PROBABILITY must be in [0,1]")
	}
	if s.MaxHypoRiskProbability < 0 || s.MaxHypoRiskProbability > 1 {
		return Settings{}, fmt.Errorf("MAX_HYPO_RISK_PROBABILITY must be in [0,1]")
	}
	if s.GlucoseUnit != "mmol" && s.GlucoseUnit != "mgdl" {
		s.GlucoseUnit = "mmol"
	}
	if s.WeeklyStatsLookbackDays < 3 {
		s.WeeklyStatsLookbackDays = 3
	}
	if s.WeeklyStatsLookbackDays > 30 {
		s.WeeklyStatsLookbackDays = 30
	}
	return s, nil
}

func envString(key, fallback string) string {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	return v
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envFloat(key string, fallback float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return n
}

func envBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func envStringList(key string) []string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return []string{}
	}
	parts := strings.Split(v, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func envInt64List(key string) []int64 {
	vals := envStringList(key)
	out := make([]int64, 0, len(vals))
	for _, raw := range vals {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}
