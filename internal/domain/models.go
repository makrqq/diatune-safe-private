package domain

import "time"

type ParameterName string

const (
	ParameterICR   ParameterName = "icr"
	ParameterISF   ParameterName = "isf"
	ParameterBasal ParameterName = "basal"
)

type TimeBlock struct {
	Name      string `json:"name"`
	StartHour int    `json:"start_hour"`
	EndHour   int    `json:"end_hour"`
}

func (b TimeBlock) ContainsHour(hour int) bool {
	if b.StartHour <= b.EndHour {
		return hour >= b.StartHour && hour <= b.EndHour
	}
	return hour >= b.StartHour || hour <= b.EndHour
}

type BlockSettings struct {
	Block TimeBlock `json:"block"`
	ICR   float64   `json:"icr"`
	ISF   float64   `json:"isf"`
	Basal float64   `json:"basal"`
}

type PatientProfile struct {
	PatientID      string          `json:"patient_id"`
	Timezone       string          `json:"timezone"`
	TargetLowMgdl  int             `json:"target_low_mgdl"`
	TargetHighMgdl int             `json:"target_high_mgdl"`
	Blocks         []BlockSettings `json:"blocks"`
}

type GlucosePoint struct {
	TS   time.Time `json:"ts"`
	Mgdl float64   `json:"mgdl"`
}

type CarbEvent struct {
	TS    time.Time `json:"ts"`
	Grams float64   `json:"grams"`
}

type InsulinEvent struct {
	TS    time.Time `json:"ts"`
	Units float64   `json:"units"`
	Kind  string    `json:"kind"`
}

type PatientDataset struct {
	Glucose []GlucosePoint `json:"glucose"`
	Carbs   []CarbEvent    `json:"carbs"`
	Insulin []InsulinEvent `json:"insulin"`
}

type BlockStats struct {
	BlockName               string   `json:"block_name"`
	Meals                   int      `json:"meals"`
	Corrections             int      `json:"corrections"`
	FastingHours            float64  `json:"fasting_hours"`
	HypoEvents              int      `json:"hypo_events"`
	HyperEvents             int      `json:"hyper_events"`
	MeanPostprandialDelta   *float64 `json:"mean_postprandial_delta,omitempty"`
	MeanCorrectionRatio     *float64 `json:"mean_correction_ratio,omitempty"`
	FastingDriftMgdlPerHour *float64 `json:"fasting_drift_mgdl_per_hour,omitempty"`
	PostprandialVariability *float64 `json:"postprandial_variability,omitempty"`
	CorrectionVariability   *float64 `json:"correction_variability,omitempty"`
	FastingSamples          int      `json:"fasting_samples"`
}

type Recommendation struct {
	ID            *int64        `json:"id,omitempty"`
	Parameter     ParameterName `json:"parameter"`
	BlockName     string        `json:"block_name"`
	CurrentValue  float64       `json:"current_value"`
	ProposedValue float64       `json:"proposed_value"`
	PercentChange float64       `json:"percent_change"`
	Confidence    float64       `json:"confidence"`
	Blocked       bool          `json:"blocked"`
	BlockedReason string        `json:"blocked_reason,omitempty"`
	Rationale     []string      `json:"rationale"`
}

type AnalysisReport struct {
	RunID            *int64           `json:"run_id,omitempty"`
	PatientID        string           `json:"patient_id"`
	GeneratedAt      time.Time        `json:"generated_at"`
	PeriodStart      time.Time        `json:"period_start"`
	PeriodEnd        time.Time        `json:"period_end"`
	GlobalHypoEvents int              `json:"global_hypo_events"`
	Warnings         []string         `json:"warnings"`
	Stats            []BlockStats     `json:"stats"`
	Recommendations  []Recommendation `json:"recommendations"`
}
