package scheduler

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"diatune-safe/internal/config"
	"diatune-safe/internal/domain"
	"diatune-safe/internal/service"
	"diatune-safe/internal/telegram"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/robfig/cron/v3"
)

type Worker struct {
	settings config.Settings
	service  *service.Service
	cron     *cron.Cron
}

func New(settings config.Settings, svc *service.Service) *Worker {
	location := time.UTC
	if strings.TrimSpace(settings.Timezone) != "" {
		loc, err := time.LoadLocation(settings.Timezone)
		if err != nil {
			log.Printf("invalid TIMEZONE=%s, fallback to UTC", settings.Timezone)
		} else {
			location = loc
		}
	}

	return &Worker{
		settings: settings,
		service:  svc,
		cron:     cron.New(cron.WithSeconds(), cron.WithLocation(location)),
	}
}

func (w *Worker) Run(ctx context.Context, patientIDs []string) error {
	analysisIDs := patientIDs
	if len(analysisIDs) == 0 {
		analysisIDs = w.settings.AutoAnalysisPatientIDs
	}

	if w.settings.AutoAnalysisEnabled && len(analysisIDs) > 0 {
		spec := "@every " + strconv.Itoa(max(1, w.settings.AutoAnalysisIntervalMinutes)) + "m"
		for _, patientID := range analysisIDs {
			pid := patientID
			_, err := w.cron.AddFunc(spec, func() {
				_, err := w.service.RunAnalysis(context.Background(), pid, w.settings.AnalysisLookbackDays, true)
				if err != nil {
					log.Printf("scheduled analysis failed patient_id=%s err=%v", pid, err)
				}
			})
			if err != nil {
				return err
			}
			log.Printf("scheduled analysis patient_id=%s spec=%s", pid, spec)
		}
	}

	if err := w.configureDailyRecommendations(analysisIDs); err != nil {
		return err
	}
	if err := w.configureWeeklyStats(analysisIDs); err != nil {
		return err
	}

	w.cron.Start()
	defer w.cron.Stop()
	<-ctx.Done()
	return nil
}

func (w *Worker) configureDailyRecommendations(defaultPatientIDs []string) error {
	if !w.settings.DailyRecommendationEnabled {
		return nil
	}
	if strings.TrimSpace(w.settings.TelegramBotToken) == "" {
		log.Printf("daily recommendation enabled, but TELEGRAM_BOT_TOKEN is empty; skipping")
		return nil
	}
	if len(w.settings.TelegramAllowedUserIDs) == 0 {
		log.Printf("daily recommendation enabled, but TELEGRAM_ALLOWED_USER_IDS is empty; skipping")
		return nil
	}

	patientIDs := w.settings.DailyRecommendationPatientIDs
	if len(patientIDs) == 0 {
		patientIDs = defaultPatientIDs
	}
	if len(patientIDs) == 0 {
		log.Printf("daily recommendation enabled, but no patient ids configured; skipping")
		return nil
	}

	hour, minute := parseDailyTime(w.settings.DailyRecommendationTime)
	spec := fmt.Sprintf("0 %d %d * * *", minute, hour)

	bot, err := tgbotapi.NewBotAPI(w.settings.TelegramBotToken)
	if err != nil {
		return fmt.Errorf("telegram bot init failed: %w", err)
	}

	_, err = w.cron.AddFunc(spec, func() {
		for _, patientID := range patientIDs {
			report, err := w.service.RunAnalysis(context.Background(), patientID, w.settings.AnalysisLookbackDays, true)
			if err != nil {
				log.Printf("daily recommendation failed patient_id=%s err=%v", patientID, err)
				continue
			}
			w.sendDailyReport(bot, report)
		}
	})
	if err != nil {
		return err
	}

	log.Printf("scheduled daily recommendations spec=%s time=%s patients=%d users=%d",
		spec, w.settings.DailyRecommendationTime, len(patientIDs), len(w.settings.TelegramAllowedUserIDs))
	return nil
}

func (w *Worker) sendDailyReport(bot *tgbotapi.BotAPI, report domain.AnalysisReport) {
	w.sendMessage(bot, telegram.FormatReportWithSettings(report, w.settings))
}

func (w *Worker) configureWeeklyStats(defaultPatientIDs []string) error {
	if !w.settings.WeeklyStatsEnabled {
		return nil
	}
	if strings.TrimSpace(w.settings.TelegramBotToken) == "" {
		log.Printf("weekly stats enabled, but TELEGRAM_BOT_TOKEN is empty; skipping")
		return nil
	}
	if len(w.settings.TelegramAllowedUserIDs) == 0 {
		log.Printf("weekly stats enabled, but TELEGRAM_ALLOWED_USER_IDS is empty; skipping")
		return nil
	}

	patientIDs := w.settings.WeeklyStatsPatientIDs
	if len(patientIDs) == 0 {
		patientIDs = defaultPatientIDs
	}
	if len(patientIDs) == 0 {
		log.Printf("weekly stats enabled, but no patient ids configured; skipping")
		return nil
	}

	hour, minute := parseDailyTime(w.settings.WeeklyStatsTime)
	weekday := parseWeekday(w.settings.WeeklyStatsDay)
	spec := fmt.Sprintf("0 %d %d * * %d", minute, hour, weekday)

	bot, err := tgbotapi.NewBotAPI(w.settings.TelegramBotToken)
	if err != nil {
		return fmt.Errorf("telegram bot init failed: %w", err)
	}

	_, err = w.cron.AddFunc(spec, func() {
		for _, patientID := range patientIDs {
			report, err := w.service.GetWeeklyStats(context.Background(), patientID, w.settings.WeeklyStatsLookbackDays, true)
			if err != nil {
				log.Printf("weekly stats failed patient_id=%s err=%v", patientID, err)
				continue
			}
			w.sendMessage(bot, telegram.FormatWeeklyStatsWithSettings(report, w.settings))
		}
	})
	if err != nil {
		return err
	}

	log.Printf("scheduled weekly stats spec=%s day=%s time=%s patients=%d users=%d",
		spec, w.settings.WeeklyStatsDay, w.settings.WeeklyStatsTime, len(patientIDs), len(w.settings.TelegramAllowedUserIDs))
	return nil
}

func (w *Worker) sendMessage(bot *tgbotapi.BotAPI, text string) {
	for _, userID := range w.settings.TelegramAllowedUserIDs {
		msg := tgbotapi.NewMessage(userID, text)
		if _, err := bot.Send(msg); err != nil {
			log.Printf("failed to send message to user_id=%d err=%v", userID, err)
		}
	}
}

func parseDailyTime(raw string) (int, int) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 22, 0
	}
	parsed, err := time.Parse("15:04", raw)
	if err != nil {
		log.Printf("invalid DAILY_RECOMMENDATION_TIME=%s, fallback to 22:00", raw)
		return 22, 0
	}
	return parsed.Hour(), parsed.Minute()
}

func parseWeekday(raw string) int {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "sun", "sunday", "вс":
		return 0
	case "mon", "monday", "пн":
		return 1
	case "tue", "tuesday", "вт":
		return 2
	case "wed", "wednesday", "ср":
		return 3
	case "thu", "thursday", "чт":
		return 4
	case "fri", "friday", "пт":
		return 5
	case "sat", "saturday", "сб":
		return 6
	default:
		log.Printf("invalid WEEKLY_STATS_DAY=%s, fallback to mon", raw)
		return 1
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
