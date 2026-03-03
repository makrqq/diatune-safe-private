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

	AutoAnalysisEnabled         bool
	AutoAnalysisIntervalMinutes int
	AutoAnalysisPatientIDs      []string
}

func Load() (Settings, error) {
	_ = godotenv.Load()

	s := Settings{
		AppEnv:       envString("APP_ENV", "development"),
		AppHost:      envString("APP_HOST", "0.0.0.0"),
		AppPort:      envInt("APP_PORT", 8080),
		AppAPIKey:    envString("APP_API_KEY", ""),
		DatabasePath: envString("DATABASE_PATH", "/workspace/data/diatune_safe.sqlite3"),
		Timezone:     envString("TIMEZONE", "UTC"),
		LogLevel:     strings.ToUpper(envString("LOG_LEVEL", "INFO")),

		TelegramBotToken:       envString("TELEGRAM_BOT_TOKEN", ""),
		TelegramAllowedUserIDs: envInt64List("TELEGRAM_ALLOWED_USER_IDS"),

		NightscoutURL:       strings.TrimRight(envString("NIGHTSCOUT_URL", ""), "/"),
		NightscoutAPISecret: envString("NIGHTSCOUT_API_SECRET", ""),

		AnalysisLookbackDays:   envInt("ANALYSIS_LOOKBACK_DAYS", 14),
		MaxDailyChangePct:      envFloat("MAX_DAILY_CHANGE_PCT", 4.0),
		MinMealsPerBlock:       envInt("MIN_MEALS_PER_BLOCK", 3),
		MinCorrectionsPerBlock: envInt("MIN_CORRECTIONS_PER_BLOCK", 3),
		MinFastingHours:        envInt("MIN_FASTING_HOURS", 6),
		HypoThresholdMgdl:      envInt("HYPO_THRESHOLD_MGDL", 70),
		HyperThresholdMgdl:     envInt("HYPER_THRESHOLD_MGDL", 180),
		GlobalHypoGuardLimit:   envInt("GLOBAL_HYPO_GUARD_LIMIT", 2),
		SafetyMinConfidence:    envFloat("SAFETY_MIN_CONFIDENCE", 0.55),

		AutoAnalysisEnabled:         envBool("AUTO_ANALYSIS_ENABLED", false),
		AutoAnalysisIntervalMinutes: envInt("AUTO_ANALYSIS_INTERVAL_MINUTES", 360),
		AutoAnalysisPatientIDs:      envStringList("AUTO_ANALYSIS_PATIENT_IDS"),
	}

	if s.AppPort <= 0 {
		return Settings{}, fmt.Errorf("APP_PORT must be positive")
	}
	if s.AnalysisLookbackDays <= 0 {
		return Settings{}, fmt.Errorf("ANALYSIS_LOOKBACK_DAYS must be positive")
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
