package telegram

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"diatune-safe/internal/config"
	"diatune-safe/internal/domain"
	"diatune-safe/internal/service"
	appversion "diatune-safe/internal/version"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Runner struct {
	settings config.Settings
	service  *service.Service
}

const telegramMessageLimit = 3500

func New(settings config.Settings, svc *service.Service) *Runner {
	return &Runner{settings: settings, service: svc}
}

func (r *Runner) Run(ctx context.Context) error {
	if strings.TrimSpace(r.settings.TelegramBotToken) == "" {
		return fmt.Errorf("переменная TELEGRAM_BOT_TOKEN не задана")
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
			fmt.Sprintf("👋 Diatune Safe %s\n\n", appversion.Semver())+
				"Я помогаю с анализом данных и формирую только предложения.\n"+
				"Ничего автоматически не применяю.\n\n"+
				"Быстрый старт:\n"+
				"1) /analyze [patient_id] [days] - свежий разбор\n"+
				"2) /pending [patient_id] - список изменений для ручной проверки\n"+
				"3) /ack <id> [reviewer] - отметить рекомендацию как проверенную\n\n"+
				"Дополнительно:\n"+
				"• /latest [patient_id] - последний отчет\n"+
				"• /backtest [patient_id] [days] - проверка на истории\n"+
				"• /weekstats [patient_id] [days] - сравнение недели к неделе\n"+
				"• /version - версия сервиса")
	case "/version":
		r.send(bot, chatID, "Версия сервиса: "+appversion.Semver())
	case "/analyze":
		patientID := fmt.Sprintf("tg-%d", userID)
		if len(args) > 0 {
			patientID = args[0]
		}
		days := r.settings.AnalysisLookbackDays
		if len(args) > 1 {
			v, err := strconv.Atoi(args[1])
			if err != nil {
				r.send(bot, chatID, "Неверный формат количества дней. Пример: /analyze demo 14")
				return
			}
			days = v
		}
		report, err := r.service.RunAnalysis(ctx, patientID, days, true)
		if err != nil {
			r.send(bot, chatID, "Не удалось выполнить анализ: "+err.Error())
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
			r.send(bot, chatID, "Не удалось получить последний отчет: "+err.Error())
			return
		}
		if report == nil {
			r.send(bot, chatID, "Отчетов пока нет. Сначала запустите /analyze.")
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
				r.send(bot, chatID, "Неверный формат количества дней. Пример: /backtest demo 42")
				return
			}
			days = v
		}
		report, err := r.service.RunBacktest(ctx, patientID, days, true)
		if err != nil {
			r.send(bot, chatID, "Не удалось выполнить проверку на истории: "+err.Error())
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
				r.send(bot, chatID, "Неверный формат количества дней. Пример: /weekstats demo 7")
				return
			}
			days = v
		}
		report, err := r.service.GetWeeklyStats(ctx, patientID, days, true)
		if err != nil {
			r.send(bot, chatID, "Не удалось собрать недельную статистику: "+err.Error())
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
			r.send(bot, chatID, "Не удалось получить список рекомендаций: "+err.Error())
			return
		}
		if len(recs) == 0 {
			r.send(bot, chatID, "Сейчас нет рекомендаций, ожидающих ручной проверки.")
			return
		}
		sort.Slice(recs, func(i, j int) bool {
			if recs[i].Blocked != recs[j].Blocked {
				return !recs[i].Blocked
			}
			return recs[i].Confidence > recs[j].Confidence
		})
		lines := []string{
			"📋 Рекомендации для ручной проверки",
			fmt.Sprintf("Всего: %d (сначала к выполнению, затем заблокированные)", len(recs)),
			"",
		}
		for i, rec := range recs {
			lines = append(lines, formatRecommendationCompact(i+1, rec, r.settings))
		}
		r.send(bot, chatID, strings.Join(lines, "\n"))
	case "/ack":
		if len(args) == 0 {
			r.send(bot, chatID, "Формат: /ack <recommendation_id> [reviewer]")
			return
		}
		recID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			r.send(bot, chatID, "ID рекомендации должен быть числом.")
			return
		}
		reviewer := fmt.Sprintf("tg:%d", userID)
		if len(args) > 1 {
			reviewer = args[1]
		}
		ok, err := r.service.AcknowledgeRecommendation(recID, reviewer)
		if err != nil {
			r.send(bot, chatID, "Не удалось отметить рекомендацию: "+err.Error())
			return
		}
		if ok {
			r.send(bot, chatID, fmt.Sprintf("✅ Рекомендация %d отмечена как проверенная (%s).", recID, reviewer))
		} else {
			r.send(bot, chatID, "Рекомендация не найдена или уже была отмечена ранее.")
		}
	default:
		r.send(bot, chatID, "Неизвестная команда. Отправьте /help, чтобы увидеть список команд.")
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
	for _, chunk := range SplitForTelegram(text, telegramMessageLimit) {
		msg := tgbotapi.NewMessage(chatID, chunk)
		_, _ = bot.Send(msg)
	}
}

func FormatReport(report domain.AnalysisReport) string {
	return FormatReportWithSettings(report, config.Settings{})
}

func FormatReportWithSettings(report domain.AnalysisReport, settings config.Settings) string {
	openRecs, blockedRecs := splitRecommendations(report.Recommendations)
	sort.Slice(openRecs, func(i, j int) bool {
		if openRecs[i].Confidence == openRecs[j].Confidence {
			return abs(openRecs[i].PercentChange) > abs(openRecs[j].PercentChange)
		}
		return openRecs[i].Confidence > openRecs[j].Confidence
	})

	lines := []string{
		fmt.Sprintf("🩺 Отчет Diatune Safe %s", appversion.Semver()),
		fmt.Sprintf("Пациент: %s", report.PatientID),
		fmt.Sprintf("Номер отчета: #%v", derefInt64(report.RunID)),
		fmt.Sprintf("Сформирован: %s", formatTS(report.GeneratedAt, settings.Timezone)),
		fmt.Sprintf("Период: %s..%s", formatDate(report.PeriodStart, settings.Timezone), formatDate(report.PeriodEnd, settings.Timezone)),
		"",
		"Коротко:",
		fmt.Sprintf("• К выполнению: %d", len(openRecs)),
		fmt.Sprintf("• Заблокировано фильтрами: %d", len(blockedRecs)),
		fmt.Sprintf("• События гипо за период: %d", report.GlobalHypoEvents),
	}
	if len(report.Warnings) > 0 {
		lines = append(lines, "", "На что обратить внимание:")
		limit := 3
		if len(report.Warnings) < limit {
			limit = len(report.Warnings)
		}
		for _, w := range report.Warnings[:limit] {
			lines = append(lines, fmt.Sprintf("• %s", w))
		}
		if len(report.Warnings) > limit {
			lines = append(lines, fmt.Sprintf("... и еще %d", len(report.Warnings)-limit))
		}
	}

	lines = append(lines, "", "Что можно изменить сейчас:")
	if len(openRecs) == 0 {
		lines = append(lines, "• Сейчас безопасных изменений не найдено.")
		lines = append(lines, "• Проверьте /weekstats или накопите больше данных.")
	} else {
		limit := 4
		if len(openRecs) < limit {
			limit = len(openRecs)
		}
		for i, rec := range openRecs[:limit] {
			lines = append(lines, formatRecommendation(i+1, rec, settings))
		}
		if len(openRecs) > limit {
			rest := len(openRecs) - limit
			lines = append(lines, fmt.Sprintf("... еще %d %s в списке /pending.", rest, recommendationWord(rest)))
		}
	}

	if len(blockedRecs) > 0 {
		lines = append(lines, "", "Почему часть изменений заблокирована:")
		for _, reasonLine := range topBlockedReasons(blockedRecs, 3) {
			lines = append(lines, "• "+reasonLine)
		}
	}
	lines = append(lines, "", "Проверьте каждое изменение вручную перед применением.")
	lines = append(lines, "Полный список: /pending [patient_id] ✅")
	return strings.Join(lines, "\n")
}

func formatRecommendation(index int, rec domain.Recommendation, settings config.Settings) string {
	status := "К выполнению"
	if rec.Blocked {
		status = "Заблокировано"
	}
	id := "-"
	if rec.ID != nil {
		id = strconv.FormatInt(*rec.ID, 10)
	}
	label := parameterLabel(rec.Parameter)
	action := recommendationAction(rec)
	change := fmt.Sprintf("%.1f", abs(rec.PercentChange))
	lines := []string{
		fmt.Sprintf("%d) [%s] %s - %s", index, rec.BlockName, label, status),
		fmt.Sprintf("   Что сделать: %s (%s%%)", action, change),
		"   " + recommendationValues(rec, settings),
		fmt.Sprintf("   Уверенность алгоритма: %.2f (id=%s)", rec.Confidence, id),
	}
	if rec.BlockedReason != "" {
		lines = append(lines, "   Причина блокировки: "+rec.BlockedReason)
		return strings.Join(lines, "\n")
	}
	lines = append(lines, "   В AAPS: "+aapsPatchLine(rec, settings))
	if len(rec.Rationale) > 0 && !rec.Blocked {
		lines = append(lines, "   Почему: "+firstSentence(rec.Rationale[0]))
	}
	return strings.Join(lines, "\n")
}

func formatRecommendationCompact(index int, rec domain.Recommendation, settings config.Settings) string {
	state := "к выполнению"
	if rec.Blocked {
		state = "заблокировано"
	}
	line := fmt.Sprintf("%d) [%s] %s | %s | %.1f%% | уверенность %.2f",
		index, rec.BlockName, parameterLabel(rec.Parameter), state, abs(rec.PercentChange), rec.Confidence)
	if rec.Blocked {
		return line + "\n   Причина: " + fallbackText(rec.BlockedReason, "ограничение безопасности")
	}
	return line + "\n   В AAPS: " + aapsPatchLine(rec, settings)
}

func SplitForTelegram(text string, maxRunes int) []string {
	if maxRunes < 256 {
		maxRunes = 256
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return []string{""}
	}

	lines := strings.Split(text, "\n")
	chunks := make([]string, 0, 4)
	current := ""
	for _, line := range lines {
		if utf8.RuneCountInString(line) > maxRunes {
			if current != "" {
				chunks = append(chunks, current)
				current = ""
			}
			parts := splitLongLine(line, maxRunes)
			chunks = append(chunks, parts...)
			continue
		}

		if current == "" {
			current = line
			continue
		}
		candidate := current + "\n" + line
		if utf8.RuneCountInString(candidate) <= maxRunes {
			current = candidate
			continue
		}
		chunks = append(chunks, current)
		current = line
	}
	if current != "" {
		chunks = append(chunks, current)
	}
	return chunks
}

func splitLongLine(line string, maxRunes int) []string {
	if utf8.RuneCountInString(line) <= maxRunes {
		return []string{line}
	}
	words := strings.Fields(line)
	if len(words) == 0 {
		return []string{line}
	}
	chunks := make([]string, 0, 2)
	current := ""
	for _, word := range words {
		if current == "" {
			current = word
			continue
		}
		candidate := current + " " + word
		if utf8.RuneCountInString(candidate) <= maxRunes {
			current = candidate
			continue
		}
		chunks = append(chunks, current)
		current = word
	}
	if current != "" {
		chunks = append(chunks, current)
	}
	return chunks
}

func FormatBacktestReport(report domain.BacktestReport) string {
	return FormatBacktestReportWithSettings(report, config.Settings{})
}

func FormatBacktestReportWithSettings(report domain.BacktestReport, settings config.Settings) string {
	tir := report.OverallMetrics.TimeInRangePct
	lines := []string{
		"📚 Проверка алгоритма на истории",
		fmt.Sprintf("Пациент: %s", report.PatientID),
		fmt.Sprintf("Период: %s..%s (%d дн., источник: %s)",
			formatDate(report.PeriodStart, settings.Timezone), formatDate(report.PeriodEnd, settings.Timezone), report.Days, dataSourceLabel(report.DataSource)),
		"",
		"Главные метрики:",
		fmt.Sprintf("• В диапазоне (TIR 3.9-10.0): %.1f%%", tir),
		fmt.Sprintf("• Ниже 3.9 ммоль/л: %.1f%%", report.OverallMetrics.Below70Pct),
		fmt.Sprintf("• Ниже 3.0 ммоль/л: %.1f%%", report.OverallMetrics.Below54Pct),
		fmt.Sprintf("• Средняя глюкоза: %.1f ммоль/л (%.1f mg/dL)",
			mgdlToMmol(report.OverallMetrics.MeanGlucoseMgdl), report.OverallMetrics.MeanGlucoseMgdl),
		fmt.Sprintf("• Вариативность (CV): %.1f%%", report.OverallMetrics.CVPct),
		fmt.Sprintf("• Прогноз HbA1c (GMI): %.2f", report.OverallMetrics.GMI),
		"",
		"Рекомендации алгоритма:",
		fmt.Sprintf("• К выполнению: %d", report.OverallRecommendations.Open),
		fmt.Sprintf("• Заблокировано: %d", report.OverallRecommendations.Blocked),
		fmt.Sprintf("• Всего: %d (средняя уверенность %.2f)",
			report.OverallRecommendations.Total, report.OverallRecommendations.AvgConfidence),
		fmt.Sprintf("• Оценка качества: %.1f/100 (%s)", report.AverageQualityScore, qualityLabel(report.AverageQualityScore)),
		"",
		"Как читать метрики:",
		"• TIR - процент времени в целевом диапазоне. Чем выше, тем лучше.",
		"• <3.9 и <3.0 - риск гипогликемии. Чем ниже, тем безопаснее.",
	}

	if len(report.Daily) > 0 {
		lines = append(lines, "", "Последние 3 дня:")
		start := len(report.Daily) - 3
		if start < 0 {
			start = 0
		}
		for _, day := range report.Daily[start:] {
			lines = append(lines, fmt.Sprintf("• %s: качество %.1f/100 | TIR %.1f%% | <3.9 %.1f%%",
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
		"📅 Сравнение недели к неделе",
		fmt.Sprintf("Пациент: %s", report.PatientID),
		fmt.Sprintf("Текущий период: %s..%s (%d дн., источник: %s)",
			formatDate(report.CurrentStart, settings.Timezone), formatDate(report.CurrentEnd, settings.Timezone),
			report.LookbackDays, dataSourceLabel(report.DataSource)),
		fmt.Sprintf("Предыдущий период: %s..%s", formatDate(report.PreviousStart, settings.Timezone), formatDate(report.PreviousEnd, settings.Timezone)),
		"",
		"Текущие показатели:",
		fmt.Sprintf("• TIR (3.9-10.0): %.1f%%", report.CurrentMetrics.TimeInRangePct),
		fmt.Sprintf("• Ниже 3.9 ммоль/л: %.1f%%", report.CurrentMetrics.Below70Pct),
		fmt.Sprintf("• Средняя глюкоза: %.1f ммоль/л", mgdlToMmol(report.CurrentMetrics.MeanGlucoseMgdl)),
		fmt.Sprintf("• CV: %.1f%%", report.CurrentMetrics.CVPct),
		"",
		"Изменение к прошлой неделе:",
		fmt.Sprintf("• TIR: %s%.1f п.п. (%s)", sign(report.DeltaTIRPct), report.DeltaTIRPct, deltaQualityLabel(report.DeltaTIRPct, true)),
		fmt.Sprintf("• <3.9: %s%.1f п.п. (%s)", sign(report.DeltaBelow70Pct), report.DeltaBelow70Pct, deltaQualityLabel(report.DeltaBelow70Pct, false)),
		fmt.Sprintf("• Средняя глюкоза: %s%.1f ммоль/л (%s)",
			sign(mgdlToMmol(report.DeltaMeanGlucoseMgdl)), mgdlToMmol(report.DeltaMeanGlucoseMgdl), deltaQualityLabel(mgdlToMmol(report.DeltaMeanGlucoseMgdl), false)),
		fmt.Sprintf("• CV: %s%.1f п.п. (%s)", sign(report.DeltaCVPct), report.DeltaCVPct, deltaQualityLabel(report.DeltaCVPct, false)),
		"",
		fmt.Sprintf("Рекомендации алгоритма: к выполнению %d, заблокировано %d, всего %d, средняя уверенность %.2f",
			report.CurrentRecommendations.Open, report.CurrentRecommendations.Blocked,
			report.CurrentRecommendations.Total, report.CurrentRecommendations.AvgConfidence),
		fmt.Sprintf("CGM-точек: текущий период %d, прошлый %d", report.CurrentMetrics.Samples, report.PreviousMetrics.Samples),
	}
	if report.PreviousMetrics.Samples < 50 {
		lines = append(lines, "Внимание: в прошлом периоде мало данных, сравнение может быть неточным.")
	}
	return strings.Join(lines, "\n")
}

func aapsPatchLine(rec domain.Recommendation, settings config.Settings) string {
	switch rec.Parameter {
	case domain.ParameterICR:
		return fmt.Sprintf("Профиль -> Углеводы/Инсулин (IC) [%s] = %.2f г/Ед", rec.BlockName, rec.ProposedValue)
	case domain.ParameterISF:
		if strings.ToLower(settings.GlucoseUnit) == "mgdl" {
			return fmt.Sprintf("Профиль -> ISF [%s] = %.2f mg/dL/Ед", rec.BlockName, rec.ProposedValue)
		}
		return fmt.Sprintf("Профиль -> ISF [%s] = %.2f ммоль/л/Ед", rec.BlockName, mgdlToMmol(rec.ProposedValue))
	case domain.ParameterBasal:
		return fmt.Sprintf("Профиль -> Базал [%s] = %.2f Ед/ч", rec.BlockName, rec.ProposedValue)
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

func splitRecommendations(recs []domain.Recommendation) ([]domain.Recommendation, []domain.Recommendation) {
	openRecs := make([]domain.Recommendation, 0, len(recs))
	blockedRecs := make([]domain.Recommendation, 0, len(recs))
	for _, rec := range recs {
		if rec.Blocked {
			blockedRecs = append(blockedRecs, rec)
		} else {
			openRecs = append(openRecs, rec)
		}
	}
	return openRecs, blockedRecs
}

func topBlockedReasons(recs []domain.Recommendation, top int) []string {
	if len(recs) == 0 {
		return []string{}
	}
	counts := map[string]int{}
	for _, rec := range recs {
		reason := strings.TrimSpace(rec.BlockedReason)
		if reason == "" {
			reason = "другая причина"
		}
		counts[reason]++
	}
	type pair struct {
		reason string
		count  int
	}
	items := make([]pair, 0, len(counts))
	for reason, count := range counts {
		items = append(items, pair{reason: reason, count: count})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].count > items[j].count })

	if top <= 0 || top > len(items) {
		top = len(items)
	}
	lines := make([]string, 0, top)
	for _, item := range items[:top] {
		lines = append(lines, fmt.Sprintf("%s (%d)", item.reason, item.count))
	}
	return lines
}

func parameterLabel(p domain.ParameterName) string {
	switch p {
	case domain.ParameterICR:
		return "УК (IC)"
	case domain.ParameterISF:
		return "ФЧИ (ISF)"
	case domain.ParameterBasal:
		return "Базал"
	default:
		return strings.ToUpper(string(p))
	}
}

func recommendationAction(rec domain.Recommendation) string {
	if rec.Blocked {
		return "изменение заблокировано"
	}
	up := rec.PercentChange > 0
	switch rec.Parameter {
	case domain.ParameterICR:
		if up {
			return "увеличить УК"
		}
		return "уменьшить УК"
	case domain.ParameterISF:
		if up {
			return "увеличить ФЧИ"
		}
		return "уменьшить ФЧИ"
	case domain.ParameterBasal:
		if up {
			return "увеличить базал"
		}
		return "уменьшить базал"
	default:
		if up {
			return "увеличить параметр"
		}
		return "уменьшить параметр"
	}
}

func recommendationValues(rec domain.Recommendation, settings config.Settings) string {
	switch rec.Parameter {
	case domain.ParameterICR:
		return fmt.Sprintf("Было/станет: %.2f -> %.2f г/Ед", rec.CurrentValue, rec.ProposedValue)
	case domain.ParameterBasal:
		return fmt.Sprintf("Было/станет: %.2f -> %.2f Ед/ч", rec.CurrentValue, rec.ProposedValue)
	case domain.ParameterISF:
		if strings.ToLower(settings.GlucoseUnit) == "mgdl" {
			return fmt.Sprintf("Было/станет: %.2f -> %.2f mg/dL/Ед", rec.CurrentValue, rec.ProposedValue)
		}
		return fmt.Sprintf("Было/станет: %.2f -> %.2f ммоль/л/Ед (%.2f -> %.2f mg/dL/Ед)",
			mgdlToMmol(rec.CurrentValue), mgdlToMmol(rec.ProposedValue), rec.CurrentValue, rec.ProposedValue)
	default:
		return fmt.Sprintf("Было/станет: %.2f -> %.2f", rec.CurrentValue, rec.ProposedValue)
	}
}

func fallbackText(v string, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	return v
}

func qualityLabel(score float64) string {
	switch {
	case score >= 80:
		return "отлично"
	case score >= 65:
		return "хорошо"
	case score >= 50:
		return "удовлетворительно"
	default:
		return "требует внимания"
	}
}

func deltaQualityLabel(delta float64, higherIsBetter bool) string {
	if higherIsBetter {
		switch {
		case delta > 1.0:
			return "лучше"
		case delta < -1.0:
			return "хуже"
		default:
			return "без существенных изменений"
		}
	}

	switch {
	case delta < -1.0:
		return "лучше"
	case delta > 1.0:
		return "хуже"
	default:
		return "без существенных изменений"
	}
}

func recommendationWord(n int) string {
	mod10 := n % 10
	mod100 := n % 100
	switch {
	case mod10 == 1 && mod100 != 11:
		return "рекомендация"
	case mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14):
		return "рекомендации"
	default:
		return "рекомендаций"
	}
}

func dataSourceLabel(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "synthetic":
		return "синтетические данные"
	case "nightscout":
		return "Nightscout"
	default:
		return fallbackText(v, "неизвестно")
	}
}

func firstSentence(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	runes := []rune(raw)
	for i, r := range runes {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		if i+1 >= len(runes) {
			return strings.TrimSpace(raw)
		}
		next := runes[i+1]
		if unicode.IsSpace(next) || unicode.IsLetter(next) {
			return strings.TrimSpace(string(runes[:i+1]))
		}
	}
	return raw
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func derefInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
