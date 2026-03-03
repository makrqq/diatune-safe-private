package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"diatune-safe/internal/api"
	"diatune-safe/internal/config"
	"diatune-safe/internal/scheduler"
	"diatune-safe/internal/service"
	"diatune-safe/internal/telegram"
	appversion "diatune-safe/internal/version"
)

func main() {
	settings, err := config.Load()
	if err != nil {
		log.Fatalf("ошибка конфигурации: %v", err)
	}
	svc, err := service.New(settings)
	if err != nil {
		log.Fatalf("ошибка инициализации сервиса: %v", err)
	}
	defer func() {
		_ = svc.Close()
	}()

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := os.Args[1]
	switch cmd {
	case "version":
		fmt.Println(appversion.Semver())
	case "api":
		fs := flag.NewFlagSet("api", flag.ExitOnError)
		host := fs.String("host", settings.AppHost, "адрес хоста")
		port := fs.Int("port", settings.AppPort, "порт")
		_ = fs.Parse(os.Args[2:])
		server := api.New(settings, svc)
		log.Printf("HTTP API запущен на %s:%d", *host, *port)
		if err := server.Run(ctx, *host, *port); err != nil {
			log.Fatalf("ошибка API: %v", err)
		}
	case "bot":
		runner := telegram.New(settings, svc)
		log.Printf("Telegram-бот запущен")
		if err := runner.Run(ctx); err != nil {
			log.Fatalf("ошибка бота: %v", err)
		}
	case "worker":
		fs := flag.NewFlagSet("worker", flag.ExitOnError)
		patients := fs.String("patients", "", "ID пациентов через запятую")
		_ = fs.Parse(os.Args[2:])
		ids := []string{}
		if strings.TrimSpace(*patients) != "" {
			for _, part := range strings.Split(*patients, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					ids = append(ids, part)
				}
			}
		}
		worker := scheduler.New(settings, svc)
		if err := worker.Run(ctx, ids); err != nil {
			log.Fatalf("ошибка worker: %v", err)
		}
	case "analyze":
		fs := flag.NewFlagSet("analyze", flag.ExitOnError)
		patientID := fs.String("patient-id", "", "ID пациента")
		days := fs.Int("days", 0, "глубина анализа в днях")
		synthetic := fs.Bool("synthetic", false, "использовать только синтетический источник")
		_ = fs.Parse(os.Args[2:])
		if strings.TrimSpace(*patientID) == "" {
			log.Fatalf("нужно указать --patient-id")
		}
		report, err := svc.RunAnalysis(ctx, *patientID, *days, !*synthetic)
		if err != nil {
			log.Fatalf("ошибка анализа: %v", err)
		}
		fmt.Printf("ID запуска: %v\n", derefInt64(report.RunID))
		fmt.Printf("Пациент: %s\n", report.PatientID)
		fmt.Printf("Период: %s - %s\n", report.PeriodStart.Format(time.RFC3339), report.PeriodEnd.Format(time.RFC3339))
		fmt.Printf("Предупреждений: %d\n", len(report.Warnings))
		for _, w := range report.Warnings {
			fmt.Printf("- %s\n", w)
		}
		fmt.Println("Рекомендации:")
		for _, rec := range report.Recommendations {
			status := "К ВЫПОЛНЕНИЮ"
			if rec.Blocked {
				status = "ЗАБЛОКИРОВАНО"
			}
			fmt.Printf("  [%s] #%v %s %s: %.2f -> %.2f (%+.1f%%)\n",
				status,
				derefInt64(rec.ID),
				rec.BlockName,
				strings.ToUpper(string(rec.Parameter)),
				rec.CurrentValue,
				rec.ProposedValue,
				rec.PercentChange,
			)
		}
	case "bootstrap":
		fs := flag.NewFlagSet("bootstrap", flag.ExitOnError)
		patientID := fs.String("patient-id", "", "ID пациента")
		_ = fs.Parse(os.Args[2:])
		if strings.TrimSpace(*patientID) == "" {
			log.Fatalf("нужно указать --patient-id")
		}
		profile, err := svc.GetProfile(*patientID)
		if err != nil {
			log.Fatalf("ошибка bootstrap: %v", err)
		}
		fmt.Printf("Профиль готов для patient_id=%s, блоков=%d\n", profile.PatientID, len(profile.Blocks))
	case "backtest":
		fs := flag.NewFlagSet("backtest", flag.ExitOnError)
		patientID := fs.String("patient-id", "", "ID пациента")
		days := fs.Int("days", 42, "глубина проверки на истории в днях")
		synthetic := fs.Bool("synthetic", false, "использовать только синтетический источник")
		_ = fs.Parse(os.Args[2:])
		if strings.TrimSpace(*patientID) == "" {
			log.Fatalf("нужно указать --patient-id")
		}
		report, err := svc.RunBacktest(ctx, *patientID, *days, !*synthetic)
		if err != nil {
			log.Fatalf("ошибка проверки на истории: %v", err)
		}
		fmt.Printf("Проверка на истории: patient=%s source=%s period=%s..%s\n",
			report.PatientID, report.DataSource, report.PeriodStart.Format("2006-01-02"), report.PeriodEnd.Format("2006-01-02"))
		fmt.Printf("TIR: %.1f%% | <70: %.1f%% | Средняя: %.1f | CV: %.1f%% | GMI: %.2f\n",
			report.OverallMetrics.TimeInRangePct, report.OverallMetrics.Below70Pct,
			report.OverallMetrics.MeanGlucoseMgdl, report.OverallMetrics.CVPct, report.OverallMetrics.GMI)
		fmt.Printf("Рекомендации: к выполнению=%d заблокировано=%d всего=%d уверенность=%.2f\n",
			report.OverallRecommendations.Open, report.OverallRecommendations.Blocked,
			report.OverallRecommendations.Total, report.OverallRecommendations.AvgConfidence)
		fmt.Printf("Оценка качества: %.1f/100\n", report.AverageQualityScore)
	case "weekstats":
		fs := flag.NewFlagSet("weekstats", flag.ExitOnError)
		patientID := fs.String("patient-id", "", "ID пациента")
		days := fs.Int("days", settings.WeeklyStatsLookbackDays, "размер одного недельного окна в днях")
		synthetic := fs.Bool("synthetic", false, "использовать только синтетический источник")
		_ = fs.Parse(os.Args[2:])
		if strings.TrimSpace(*patientID) == "" {
			log.Fatalf("нужно указать --patient-id")
		}
		report, err := svc.GetWeeklyStats(ctx, *patientID, *days, !*synthetic)
		if err != nil {
			log.Fatalf("ошибка недельной статистики: %v", err)
		}
		fmt.Printf("Недельная статистика: patient=%s source=%s\n", report.PatientID, report.DataSource)
		fmt.Printf("Текущее окно: %s..%s\n", report.CurrentStart.Format("2006-01-02"), report.CurrentEnd.Format("2006-01-02"))
		fmt.Printf("TIR: %.1f%% (%+.1fpp) | <70: %.1f%% (%+.1fpp)\n",
			report.CurrentMetrics.TimeInRangePct, report.DeltaTIRPct,
			report.CurrentMetrics.Below70Pct, report.DeltaBelow70Pct)
		fmt.Printf("Средняя: %.1f (%+.1f) | CV: %.1f%% (%+.1fpp)\n",
			report.CurrentMetrics.MeanGlucoseMgdl, report.DeltaMeanGlucoseMgdl,
			report.CurrentMetrics.CVPct, report.DeltaCVPct)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf("Diatune Safe %s\n", appversion.Semver())
	fmt.Println("Использование: diatune-safe <команда> [флаги]")
	fmt.Println("Команды:")
	fmt.Println("  version   Показать версию сервиса")
	fmt.Println("  api       Запустить HTTP API")
	fmt.Println("  bot       Запустить Telegram-бота")
	fmt.Println("  worker    Запустить планировщик анализа")
	fmt.Println("  analyze   Выполнить разовый анализ")
	fmt.Println("  bootstrap Создать профиль пациента по умолчанию")
	fmt.Println("  backtest  Выполнить проверку на истории Nightscout/синтетике")
	fmt.Println("  weekstats Показать сравнение текущей и прошлой недели")
}

func derefInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
