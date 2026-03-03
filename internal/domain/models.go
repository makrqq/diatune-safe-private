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
	AvgMealCarbs            *float64 `json:"avg_meal_carbs,omitempty"`
	AvgCorrectionUnits      *float64 `json:"avg_correction_units,omitempty"`
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

type GlycemicMetrics struct {
	Samples         int     `json:"samples"`
	MeanGlucoseMgdl float64 `json:"mean_glucose_mgdl"`
	StdDevMgdl      float64 `json:"stddev_mgdl"`
	CVPct           float64 `json:"cv_pct"`
	GMI             float64 `json:"gmi"`

	TimeInRangePct float64 `json:"time_in_range_pct"`
	Below70Pct     float64 `json:"below_70_pct"`
	Below54Pct     float64 `json:"below_54_pct"`
	Above180Pct    float64 `json:"above_180_pct"`
	Above250Pct    float64 `json:"above_250_pct"`

	HypoEvents int `json:"hypo_events"`
}

type RecommendationStats struct {
	Total         int     `json:"total"`
	Open          int     `json:"open"`
	Blocked       int     `json:"blocked"`
	AvgConfidence float64 `json:"avg_confidence"`
}

type BacktestDayResult struct {
	Date            string              `json:"date"`
	Metrics         GlycemicMetrics     `json:"metrics"`
	Recommendations RecommendationStats `json:"recommendations"`
	QualityScore    float64             `json:"quality_score"`
}

type BacktestReport struct {
	PatientID  string    `json:"patient_id"`
	Generated  time.Time `json:"generated"`
	DataSource string    `json:"data_source"`

	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	Days        int       `json:"days"`

	OverallMetrics         GlycemicMetrics     `json:"overall_metrics"`
	OverallRecommendations RecommendationStats `json:"overall_recommendations"`
	AverageQualityScore    float64             `json:"average_quality_score"`
	Daily                  []BacktestDayResult `json:"daily"`
}

type WeeklyStatsReport struct {
	PatientID  string    `json:"patient_id"`
	Generated  time.Time `json:"generated"`
	DataSource string    `json:"data_source"`

	LookbackDays int `json:"lookback_days"`

	CurrentStart  time.Time `json:"current_start"`
	CurrentEnd    time.Time `json:"current_end"`
	PreviousStart time.Time `json:"previous_start"`
	PreviousEnd   time.Time `json:"previous_end"`

	CurrentMetrics         GlycemicMetrics     `json:"current_metrics"`
	PreviousMetrics        GlycemicMetrics     `json:"previous_metrics"`
	CurrentRecommendations RecommendationStats `json:"current_recommendations"`

	DeltaTIRPct          float64 `json:"delta_tir_pct"`
	DeltaBelow70Pct      float64 `json:"delta_below_70_pct"`
	DeltaMeanGlucoseMgdl float64 `json:"delta_mean_glucose_mgdl"`
	DeltaCVPct           float64 `json:"delta_cv_pct"`
}
