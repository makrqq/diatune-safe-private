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
)

func main() {
	settings, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}
	svc, err := service.New(settings)
	if err != nil {
		log.Fatalf("service init error: %v", err)
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
	case "api":
		fs := flag.NewFlagSet("api", flag.ExitOnError)
		host := fs.String("host", settings.AppHost, "host")
		port := fs.Int("port", settings.AppPort, "port")
		_ = fs.Parse(os.Args[2:])
		server := api.New(settings, svc)
		log.Printf("API listening on %s:%d", *host, *port)
		if err := server.Run(ctx, *host, *port); err != nil {
			log.Fatalf("api error: %v", err)
		}
	case "bot":
		runner := telegram.New(settings, svc)
		log.Printf("Telegram bot started")
		if err := runner.Run(ctx); err != nil {
			log.Fatalf("bot error: %v", err)
		}
	case "worker":
		fs := flag.NewFlagSet("worker", flag.ExitOnError)
		patients := fs.String("patients", "", "comma-separated patient ids")
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
			log.Fatalf("worker error: %v", err)
		}
	case "analyze":
		fs := flag.NewFlagSet("analyze", flag.ExitOnError)
		patientID := fs.String("patient-id", "", "patient id")
		days := fs.Int("days", 0, "lookback days")
		synthetic := fs.Bool("synthetic", false, "force synthetic source")
		_ = fs.Parse(os.Args[2:])
		if strings.TrimSpace(*patientID) == "" {
			log.Fatalf("--patient-id is required")
		}
		report, err := svc.RunAnalysis(ctx, *patientID, *days, !*synthetic)
		if err != nil {
			log.Fatalf("analyze error: %v", err)
		}
		fmt.Printf("Run ID: %v\n", derefInt64(report.RunID))
		fmt.Printf("Patient: %s\n", report.PatientID)
		fmt.Printf("Period: %s - %s\n", report.PeriodStart.Format(time.RFC3339), report.PeriodEnd.Format(time.RFC3339))
		fmt.Printf("Warnings: %d\n", len(report.Warnings))
		for _, w := range report.Warnings {
			fmt.Printf("- %s\n", w)
		}
		fmt.Println("Recommendations:")
		for _, rec := range report.Recommendations {
			status := "OPEN"
			if rec.Blocked {
				status = "BLOCKED"
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
		patientID := fs.String("patient-id", "", "patient id")
		_ = fs.Parse(os.Args[2:])
		if strings.TrimSpace(*patientID) == "" {
			log.Fatalf("--patient-id is required")
		}
		profile, err := svc.GetProfile(*patientID)
		if err != nil {
			log.Fatalf("bootstrap error: %v", err)
		}
		fmt.Printf("Profile ready for patient_id=%s, blocks=%d\n", profile.PatientID, len(profile.Blocks))
	case "backtest":
		fs := flag.NewFlagSet("backtest", flag.ExitOnError)
		patientID := fs.String("patient-id", "", "patient id")
		days := fs.Int("days", 42, "backtest days")
		synthetic := fs.Bool("synthetic", false, "force synthetic source")
		_ = fs.Parse(os.Args[2:])
		if strings.TrimSpace(*patientID) == "" {
			log.Fatalf("--patient-id is required")
		}
		report, err := svc.RunBacktest(ctx, *patientID, *days, !*synthetic)
		if err != nil {
			log.Fatalf("backtest error: %v", err)
		}
		fmt.Printf("Backtest patient=%s source=%s period=%s..%s\n",
			report.PatientID, report.DataSource, report.PeriodStart.Format("2006-01-02"), report.PeriodEnd.Format("2006-01-02"))
		fmt.Printf("TIR: %.1f%% | <70: %.1f%% | Mean: %.1f | CV: %.1f%% | GMI: %.2f\n",
			report.OverallMetrics.TimeInRangePct, report.OverallMetrics.Below70Pct,
			report.OverallMetrics.MeanGlucoseMgdl, report.OverallMetrics.CVPct, report.OverallMetrics.GMI)
		fmt.Printf("Recommendations open=%d blocked=%d total=%d conf=%.2f\n",
			report.OverallRecommendations.Open, report.OverallRecommendations.Blocked,
			report.OverallRecommendations.Total, report.OverallRecommendations.AvgConfidence)
		fmt.Printf("Average quality score: %.1f/100\n", report.AverageQualityScore)
	case "weekstats":
		fs := flag.NewFlagSet("weekstats", flag.ExitOnError)
		patientID := fs.String("patient-id", "", "patient id")
		days := fs.Int("days", settings.WeeklyStatsLookbackDays, "lookback days for one weekly window")
		synthetic := fs.Bool("synthetic", false, "force synthetic source")
		_ = fs.Parse(os.Args[2:])
		if strings.TrimSpace(*patientID) == "" {
			log.Fatalf("--patient-id is required")
		}
		report, err := svc.GetWeeklyStats(ctx, *patientID, *days, !*synthetic)
		if err != nil {
			log.Fatalf("weekstats error: %v", err)
		}
		fmt.Printf("Weekly stats patient=%s source=%s\n", report.PatientID, report.DataSource)
		fmt.Printf("Current window: %s..%s\n", report.CurrentStart.Format("2006-01-02"), report.CurrentEnd.Format("2006-01-02"))
		fmt.Printf("TIR: %.1f%% (%+.1fpp) | <70: %.1f%% (%+.1fpp)\n",
			report.CurrentMetrics.TimeInRangePct, report.DeltaTIRPct,
			report.CurrentMetrics.Below70Pct, report.DeltaBelow70Pct)
		fmt.Printf("Mean: %.1f (%+.1f) | CV: %.1f%% (%+.1fpp)\n",
			report.CurrentMetrics.MeanGlucoseMgdl, report.DeltaMeanGlucoseMgdl,
			report.CurrentMetrics.CVPct, report.DeltaCVPct)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: diatune-safe <command> [flags]")
	fmt.Println("Commands:")
	fmt.Println("  api       Run HTTP API server")
	fmt.Println("  bot       Run Telegram bot")
	fmt.Println("  worker    Run scheduled analysis worker")
	fmt.Println("  analyze   Run one-shot analysis")
	fmt.Println("  bootstrap Create default profile for patient")
	fmt.Println("  backtest  Run historical validation on Nightscout/synthetic data")
	fmt.Println("  weekstats Show current-vs-previous weekly glycemic stats")
}

func derefInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
