package scheduler

import (
	"context"
	"log"
	"strconv"

	"diatune-safe/internal/config"
	"diatune-safe/internal/service"

	"github.com/robfig/cron/v3"
)

type Worker struct {
	settings config.Settings
	service  *service.Service
	cron     *cron.Cron
}

func New(settings config.Settings, svc *service.Service) *Worker {
	return &Worker{
		settings: settings,
		service:  svc,
		cron:     cron.New(cron.WithSeconds()),
	}
}

func (w *Worker) Run(ctx context.Context, patientIDs []string) error {
	ids := patientIDs
	if len(ids) == 0 {
		ids = w.settings.AutoAnalysisPatientIDs
	}
	if len(ids) == 0 {
		return nil
	}

	spec := "@every " + strconv.Itoa(max(1, w.settings.AutoAnalysisIntervalMinutes)) + "m"
	for _, patientID := range ids {
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

	w.cron.Start()
	defer w.cron.Stop()
	<-ctx.Done()
	return nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
