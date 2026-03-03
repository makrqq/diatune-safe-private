package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"diatune-safe/internal/config"
	"diatune-safe/internal/domain"
	"diatune-safe/internal/service"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Runner struct {
	settings config.Settings
	service  *service.Service
}

func New(settings config.Settings, svc *service.Service) *Runner {
	return &Runner{settings: settings, service: svc}
}

func (r *Runner) Run(ctx context.Context) error {
	if strings.TrimSpace(r.settings.TelegramBotToken) == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN is empty")
	}
	bot, err := tgbotapi.NewBotAPI(r.settings.TelegramBotToken)
	if err != nil {
		return err
	}
	updates := bot.GetUpdatesChan(tgbotapi.NewUpdate(0))

	for {
		select {
		case <-ctx.Done():
			return nil
		case update := <-updates:
			if update.Message == nil {
				continue
			}
			if !r.allowed(update.Message.From.ID) {
				r.send(bot, update.Message.Chat.ID, "Доступ запрещен.")
				continue
			}
			text := strings.TrimSpace(update.Message.Text)
			if text == "" {
				continue
			}
			parts := strings.Fields(text)
			cmd := strings.ToLower(parts[0])
			args := []string{}
			if len(parts) > 1 {
				args = parts[1:]
			}
			r.handleCommand(ctx, bot, update.Message.Chat.ID, update.Message.From.ID, cmd, args)
		}
	}
}

func (r *Runner) handleCommand(ctx context.Context, bot *tgbotapi.BotAPI, chatID int64, userID int64, cmd string, args []string) {
	switch cmd {
	case "/start", "/help":
		r.send(bot, chatID,
			"Diatune Safe Bot\n"+
				"Команды:\n"+
				"/analyze [patient_id] [days]\n"+
				"/backtest [patient_id] [days]\n"+
				"/weekstats [patient_id] [days]\n"+
				"/latest [patient_id]\n"+
				"/pending [patient_id]\n"+
				"/ack <recommendation_id> [reviewer]\n\n"+
				"Сервис только предлагает изменения и никогда не применяет их автоматически.")
	case "/analyze":
		patientID := fmt.Sprintf("tg-%d", userID)
		if len(args) > 0 {
			patientID = args[0]
		}
		days := r.settings.AnalysisLookbackDays
		if len(args) > 1 {
			v, err := strconv.Atoi(args[1])
			if err != nil {
				r.send(bot, chatID, "Неверный формат days. Пример: /analyze demo 14")
				return
			}
			days = v
		}
		report, err := r.service.RunAnalysis(ctx, patientID, days, true)
		if err != nil {
			r.send(bot, chatID, "Ошибка анализа: "+err.Error())
			return
		}
		r.send(bot, chatID, FormatReportWithSettings(report, r.settings))
	case "/latest":
		patientID := fmt.Sprintf("tg-%d", userID)
		if len(args) > 0 {
			patientID = args[0]
		}
		report, err := r.service.GetLatestReport(patientID)
		if err != nil {
			r.send(bot, chatID, "Ошибка: "+err.Error())
			return
		}
		if report == nil {
			r.send(bot, chatID, "Отчеты не найдены. Выполните /analyze.")
			return
		}
		r.send(bot, chatID, FormatReportWithSettings(*report, r.settings))
	case "/backtest":
		patientID := fmt.Sprintf("tg-%d", userID)
		if len(args) > 0 {
			patientID = args[0]
		}
		days := 42
		if len(args) > 1 {
			v, err := strconv.Atoi(args[1])
			if err != nil {
				r.send(bot, chatID, "Неверный формат days. Пример: /backtest demo 42")
				return
			}
			days = v
		}
		report, err := r.service.RunBacktest(ctx, patientID, days, true)
		if err != nil {
			r.send(bot, chatID, "Ошибка бэктеста: "+err.Error())
			return
		}
		r.send(bot, chatID, FormatBacktestReportWithSettings(report, r.settings))
	case "/weekstats":
		patientID := fmt.Sprintf("tg-%d", userID)
		if len(args) > 0 {
			patientID = args[0]
		}
		days := r.settings.WeeklyStatsLookbackDays
		if days <= 0 {
			days = 7
		}
		if len(args) > 1 {
			v, err := strconv.Atoi(args[1])
			if err != nil {
				r.send(bot, chatID, "Неверный формат days. Пример: /weekstats demo 7")
				return
			}
			days = v
		}
		report, err := r.service.GetWeeklyStats(ctx, patientID, days, true)
		if err != nil {
			r.send(bot, chatID, "Ошибка weekly stats: "+err.Error())
			return
		}
		r.send(bot, chatID, FormatWeeklyStatsWithSettings(report, r.settings))
	case "/pending":
		patientID := fmt.Sprintf("tg-%d", userID)
		if len(args) > 0 {
			patientID = args[0]
		}
		recs, err := r.service.ListPendingRecommendations(patientID, 20)
		if err != nil {
			r.send(bot, chatID, "Ошибка: "+err.Error())
			return
		}
		if len(recs) == 0 {
			r.send(bot, chatID, "Нет ожидающих подтверждения рекомендаций.")
			return
		}
		lines := []string{"Pending recommendations:"}
		for _, rec := range recs {
			lines = append(lines, formatRecommendation(rec, r.settings))
		}
		r.send(bot, chatID, strings.Join(lines, "\n"))
	case "/ack":
		if len(args) == 0 {
			r.send(bot, chatID, "Использование: /ack <recommendation_id> [reviewer]")
			return
		}
		recID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			r.send(bot, chatID, "recommendation_id должен быть числом.")
			return
		}
		reviewer := fmt.Sprintf("tg:%d", userID)
		if len(args) > 1 {
			reviewer = args[1]
		}
		ok, err := r.service.AcknowledgeRecommendation(recID, reviewer)
		if err != nil {
			r.send(bot, chatID, "Ошибка: "+err.Error())
			return
		}
		if ok {
			r.send(bot, chatID, fmt.Sprintf("Рекомендация %d подтверждена (%s).", recID, reviewer))
		} else {
			r.send(bot, chatID, "Рекомендация не найдена или уже подтверждена.")
		}
	default:
		r.send(bot, chatID, "Неизвестная команда. Используйте /help")
	}
}

func (r *Runner) allowed(userID int64) bool {
	if len(r.settings.TelegramAllowedUserIDs) == 0 {
		return true
	}
	for _, id := range r.settings.TelegramAllowedUserIDs {
		if id == userID {
			return true
		}
	}
	return false
}

func (r *Runner) send(bot *tgbotapi.BotAPI, chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	_, _ = bot.Send(msg)
}

func FormatReport(report domain.AnalysisReport) string {
	return FormatReportWithSettings(report, config.Settings{})
}

func FormatReportWithSettings(report domain.AnalysisReport, settings config.Settings) string {
	lines := []string{
		fmt.Sprintf("Отчет #%v [%s]", derefInt64(report.RunID), report.PatientID),
		fmt.Sprintf("Сформирован: %s", formatTS(report.GeneratedAt, settings.Timezone)),
		fmt.Sprintf("Период: %s..%s", formatDate(report.PeriodStart, settings.Timezone), formatDate(report.PeriodEnd, settings.Timezone)),
		fmt.Sprintf("Гипо-эпизоды за период: %d", report.GlobalHypoEvents),
	}
	if len(report.Warnings) > 0 {
		lines = append(lines, "Предупреждения:")
		for _, w := range report.Warnings {
			lines = append(lines, "- "+w)
		}
	}
	lines = append(lines, "Рекомендации для профиля AAPS:")
	for _, rec := range report.Recommendations {
		lines = append(lines, formatRecommendation(rec, settings))
	}
	lines = append(lines, "Важно: это только предложения, ручное решение обязательно.")
	return strings.Join(lines, "\n")
}

func formatRecommendation(rec domain.Recommendation, settings config.Settings) string {
	status := "OPEN"
	if rec.Blocked {
		status = "BLOCKED"
	}
	sign := ""
	if rec.PercentChange > 0 {
		sign = "+"
	}
	id := "-"
	if rec.ID != nil {
		id = strconv.FormatInt(*rec.ID, 10)
	}
	paramTitle := strings.ToUpper(string(rec.Parameter))
	if rec.Parameter == domain.ParameterICR {
		paramTitle = "IC"
	}
	line := fmt.Sprintf("#%s %s [%s] %s: %.2f -> %.2f (%s%.1f%%, conf=%.2f)",
		id, status, rec.BlockName, paramTitle, rec.CurrentValue, rec.ProposedValue, sign, rec.PercentChange, rec.Confidence)
	if rec.Parameter == domain.ParameterISF {
		line += fmt.Sprintf(" | %.2f -> %.2f mmol/L/U",
			mgdlToMmol(rec.CurrentValue), mgdlToMmol(rec.ProposedValue))
	}
	line += " | AAPS: " + aapsPatchLine(rec, settings)
	if rec.BlockedReason != "" {
		line += " | " + rec.BlockedReason
	}
	if len(rec.Rationale) > 0 {
		maxItems := 2
		if len(rec.Rationale) < 2 {
			maxItems = len(rec.Rationale)
		}
		line += " | " + strings.Join(rec.Rationale[:maxItems], "; ")
	}
	return line
}

func FormatBacktestReport(report domain.BacktestReport) string {
	return FormatBacktestReportWithSettings(report, config.Settings{})
}

func FormatBacktestReportWithSettings(report domain.BacktestReport, settings config.Settings) string {
	lines := []string{
		fmt.Sprintf("Бэктест [%s]", report.PatientID),
		fmt.Sprintf("Период: %s..%s (%d дн., source=%s)",
			formatDate(report.PeriodStart, settings.Timezone), formatDate(report.PeriodEnd, settings.Timezone), report.Days, report.DataSource),
		fmt.Sprintf("TIR 3.9-10.0 mmol/L (70-180): %.1f%% | <3.9: %.1f%% | <3.0: %.1f%%",
			report.OverallMetrics.TimeInRangePct, report.OverallMetrics.Below70Pct, report.OverallMetrics.Below54Pct),
		fmt.Sprintf("Средняя: %.1f mmol/L (%.1f mg/dL) | CV: %.1f%% | GMI: %.2f",
			mgdlToMmol(report.OverallMetrics.MeanGlucoseMgdl), report.OverallMetrics.MeanGlucoseMgdl, report.OverallMetrics.CVPct, report.OverallMetrics.GMI),
		fmt.Sprintf("Рекомендации: open=%d blocked=%d total=%d conf=%.2f",
			report.OverallRecommendations.Open, report.OverallRecommendations.Blocked,
			report.OverallRecommendations.Total, report.OverallRecommendations.AvgConfidence),
		fmt.Sprintf("Внутренний quality-score: %.1f/100", report.AverageQualityScore),
	}

	if len(report.Daily) > 0 {
		lines = append(lines, "Последние дни:")
		start := len(report.Daily) - 3
		if start < 0 {
			start = 0
		}
		for _, day := range report.Daily[start:] {
			lines = append(lines, fmt.Sprintf("- %s: score %.1f | TIR %.1f%% | <3.9 %.1f%%",
				day.Date, day.QualityScore, day.Metrics.TimeInRangePct, day.Metrics.Below70Pct))
		}
	}
	return strings.Join(lines, "\n")
}

func FormatWeeklyStats(report domain.WeeklyStatsReport) string {
	return FormatWeeklyStatsWithSettings(report, config.Settings{})
}

func FormatWeeklyStatsWithSettings(report domain.WeeklyStatsReport, settings config.Settings) string {
	sign := func(v float64) string {
		if v >= 0 {
			return "+"
		}
		return ""
	}
	lines := []string{
		fmt.Sprintf("Еженедельная статистика [%s]", report.PatientID),
		fmt.Sprintf("Текущий период: %s..%s (%d дн., source=%s)",
			formatDate(report.CurrentStart, settings.Timezone), formatDate(report.CurrentEnd, settings.Timezone),
			report.LookbackDays, report.DataSource),
		fmt.Sprintf("CGM сэмплы: текущая=%d, прошлая=%d", report.CurrentMetrics.Samples, report.PreviousMetrics.Samples),
		fmt.Sprintf("TIR 3.9-10.0: %.1f%% (%s%.1f п.п.)", report.CurrentMetrics.TimeInRangePct, sign(report.DeltaTIRPct), report.DeltaTIRPct),
		fmt.Sprintf("<3.9: %.1f%% (%s%.1f п.п.)", report.CurrentMetrics.Below70Pct, sign(report.DeltaBelow70Pct), report.DeltaBelow70Pct),
		fmt.Sprintf("Средняя: %.1f mmol/L (%s%.1f mmol/L)",
			mgdlToMmol(report.CurrentMetrics.MeanGlucoseMgdl), sign(mgdlToMmol(report.DeltaMeanGlucoseMgdl)), mgdlToMmol(report.DeltaMeanGlucoseMgdl)),
		fmt.Sprintf("CV: %.1f%% (%s%.1fpp)", report.CurrentMetrics.CVPct, sign(report.DeltaCVPct), report.DeltaCVPct),
		fmt.Sprintf("Рекомендации алгоритма: open=%d blocked=%d total=%d conf=%.2f",
			report.CurrentRecommendations.Open, report.CurrentRecommendations.Blocked,
			report.CurrentRecommendations.Total, report.CurrentRecommendations.AvgConfidence),
	}
	if report.PreviousMetrics.Samples < 50 {
		lines = append(lines, "Внимание: в прошлом периоде мало данных, дельты могут быть нерепрезентативны.")
	}
	return strings.Join(lines, "\n")
}

func aapsPatchLine(rec domain.Recommendation, settings config.Settings) string {
	switch rec.Parameter {
	case domain.ParameterICR:
		return fmt.Sprintf("Профиль -> Carbs/IC [%s] = %.2f g/U", rec.BlockName, rec.ProposedValue)
	case domain.ParameterISF:
		if strings.ToLower(settings.GlucoseUnit) == "mgdl" {
			return fmt.Sprintf("Профиль -> ISF [%s] = %.2f mg/dL/U", rec.BlockName, rec.ProposedValue)
		}
		return fmt.Sprintf("Профиль -> ISF [%s] = %.2f mmol/L/U", rec.BlockName, mgdlToMmol(rec.ProposedValue))
	case domain.ParameterBasal:
		return fmt.Sprintf("Профиль -> Basal [%s] = %.2f U/h", rec.BlockName, rec.ProposedValue)
	default:
		return fmt.Sprintf("[%s] = %.2f", rec.BlockName, rec.ProposedValue)
	}
}

func mgdlToMmol(v float64) float64 {
	return v / 18.0
}

func formatTS(ts time.Time, tz string) string {
	loc := resolveLoc(tz)
	return ts.In(loc).Format("02.01.2006 15:04")
}

func formatDate(ts time.Time, tz string) string {
	loc := resolveLoc(tz)
	return ts.In(loc).Format("02.01.2006")
}

func resolveLoc(tz string) *time.Location {
	if strings.TrimSpace(tz) == "" {
		tz = "Europe/Moscow"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.FixedZone("MSK", 3*60*60)
	}
	return loc
}

func derefInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
