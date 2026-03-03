package main

import (
  "context"
  "fmt"
  "os"
  "diatune-safe/internal/config"
  "diatune-safe/internal/service"
)

func main() {
  s, err := config.Load()
  if err != nil { panic(err) }
  svc, err := service.New(s)
  if err != nil { panic(err) }
  defer svc.Close()
  r, err := svc.GetWeeklyStats(context.Background(), "patient-1", 7, true)
  if err != nil { panic(err) }
  fmt.Printf("source=%s\n", r.DataSource)
  fmt.Printf("current %s..%s samples=%d TIR=%.1f <70=%.1f\n", r.CurrentStart.Format("2006-01-02"), r.CurrentEnd.Format("2006-01-02"), r.CurrentMetrics.Samples, r.CurrentMetrics.TimeInRangePct, r.CurrentMetrics.Below70Pct)
  fmt.Printf("previous %s..%s samples=%d TIR=%.1f <70=%.1f\n", r.PreviousStart.Format("2006-01-02"), r.PreviousEnd.Format("2006-01-02"), r.PreviousMetrics.Samples, r.PreviousMetrics.TimeInRangePct, r.PreviousMetrics.Below70Pct)
  fmt.Printf("delta TIR=%+.1f delta<70=%+.1f\n", r.DeltaTIRPct, r.DeltaBelow70Pct)
  _ = os.Remove("/workspace/.tmp_weekstats.go")
}
