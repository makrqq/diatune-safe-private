package datasource

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"diatune-safe/internal/domain"
)

type Source interface {
	FetchDataset(ctx context.Context, patientID string, since, until time.Time) (domain.PatientDataset, error)
}

type NightscoutSource struct {
	BaseURL    string
	APISecret  string
	Timeout    time.Duration
	HTTPClient *http.Client
}

type DataSourceError struct {
	Msg string
}

func (e DataSourceError) Error() string {
	return e.Msg
}

func (s NightscoutSource) FetchDataset(ctx context.Context, patientID string, since, until time.Time) (domain.PatientDataset, error) {
	_ = patientID
	client := s.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: s.Timeout}
		if client.Timeout == 0 {
			client.Timeout = 30 * time.Second
		}
	}
	if strings.TrimSpace(s.BaseURL) == "" {
		return domain.PatientDataset{}, DataSourceError{Msg: "пустой URL Nightscout"}
	}

	entriesURL := buildURL(s.BaseURL+"/api/v1/entries/sgv.json", since, until)
	treatmentsURL := buildURL(s.BaseURL+"/api/v1/treatments.json", since, until)
	headers := http.Header{}
	if h := apiSecretHeader(s.APISecret); h != "" {
		headers.Set("API-SECRET", h)
	}

	entriesResp, err := doJSONRequest(ctx, client, entriesURL, headers)
	if err != nil {
		return domain.PatientDataset{}, err
	}
	treatResp, err := doJSONRequest(ctx, client, treatmentsURL, headers)
	if err != nil {
		return domain.PatientDataset{}, err
	}

	glucose := parseGlucose(entriesResp)
	carbs, insulin := parseTreatments(treatResp)
	return domain.PatientDataset{Glucose: glucose, Carbs: carbs, Insulin: insulin}, nil
}

type SyntheticSource struct {
	Seed int64
}

func (s SyntheticSource) FetchDataset(ctx context.Context, patientID string, since, until time.Time) (domain.PatientDataset, error) {
	_ = ctx
	if s.Seed == 0 {
		s.Seed = 7
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(patientID + ":" + since.Format("2006-01-02") + ":" + until.Format("2006-01-02")))
	seed := int64(h.Sum64()) + s.Seed
	rng := rand.New(rand.NewSource(seed))

	glucose := make([]domain.GlucosePoint, 0, int(until.Sub(since).Minutes()/5)+1)
	carbs := []domain.CarbEvent{}
	insulin := []domain.InsulinEvent{}

	cursor := since
	for !cursor.After(until) {
		dayFraction := float64(cursor.Hour()*60+cursor.Minute()) / float64(24*60)
		circadian := 12 * math.Sin(2*math.Pi*dayFraction-1.2)
		baseline := 118 + circadian
		noise := rng.NormFloat64() * 9
		mgdl := baseline + noise
		if mgdl < 55 {
			mgdl = 55
		}
		if mgdl > 280 {
			mgdl = 280
		}
		glucose = append(glucose, domain.GlucosePoint{TS: cursor, Mgdl: mgdl})
		cursor = cursor.Add(5 * time.Minute)
	}

	dayStart := time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, since.Location())
	mealHours := []int{8, 13, 19}
	for day := dayStart; day.Before(until); day = day.Add(24 * time.Hour) {
		for _, hour := range mealHours {
			mealTS := day.Add(time.Duration(hour) * time.Hour).Add(time.Duration(rng.Intn(41)-20) * time.Minute)
			if mealTS.Before(since) || mealTS.After(until) {
				continue
			}
			grams := clamp(rng.NormFloat64()*14+52, 20, 95)
			carbs = append(carbs, domain.CarbEvent{TS: mealTS, Grams: grams})
			insulinUnits := grams / (8 + rng.Float64()*6)
			if insulinUnits < 1 {
				insulinUnits = 1
			}
			insulin = append(insulin, domain.InsulinEvent{
				TS:    mealTS.Add(-12 * time.Minute),
				Units: insulinUnits,
				Kind:  "bolus",
			})
		}
		if rng.Float64() > 0.4 {
			correctionTS := day.Add(time.Duration(rng.Intn(24)) * time.Hour).Add(time.Duration(rng.Intn(60)) * time.Minute)
			if !correctionTS.Before(since) && !correctionTS.After(until) {
				insulin = append(insulin, domain.InsulinEvent{
					TS:    correctionTS,
					Units: 0.8 + rng.Float64()*1.6,
					Kind:  "bolus",
				})
			}
		}
	}

	return domain.PatientDataset{Glucose: glucose, Carbs: carbs, Insulin: insulin}, nil
}

func buildURL(base string, since, until time.Time) string {
	u, _ := url.Parse(base)
	q := u.Query()
	q.Set("find[created_at][$gte]", since.UTC().Format(time.RFC3339))
	q.Set("find[created_at][$lte]", until.UTC().Format(time.RFC3339))
	q.Set("count", "100000")
	u.RawQuery = q.Encode()
	return u.String()
}

func apiSecretHeader(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	lower := strings.ToLower(secret)
	if len(lower) == 40 {
		isHex := true
		for _, c := range lower {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				isHex = false
				break
			}
		}
		if isHex {
			return lower
		}
	}
	sum := sha1.Sum([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func doJSONRequest(ctx context.Context, client *http.Client, endpoint string, headers http.Header) ([]map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, DataSourceError{Msg: err.Error()}
	}
	req.Header = headers.Clone()
	resp, err := client.Do(req)
	if err != nil {
		return nil, DataSourceError{Msg: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, DataSourceError{Msg: fmt.Sprintf("ошибка запроса к Nightscout: %d", resp.StatusCode)}
	}
	var payload []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, DataSourceError{Msg: err.Error()}
	}
	return payload, nil
}

func parseGlucose(rows []map[string]any) []domain.GlucosePoint {
	out := []domain.GlucosePoint{}
	for _, row := range rows {
		ts, ok := extractTime(row)
		if !ok {
			continue
		}
		raw, ok := row["sgv"]
		if !ok {
			raw = row["mbg"]
		}
		mgdl, ok := toFloat(raw)
		if !ok {
			continue
		}
		out = append(out, domain.GlucosePoint{TS: ts, Mgdl: mgdl})
	}
	return out
}

func parseTreatments(rows []map[string]any) ([]domain.CarbEvent, []domain.InsulinEvent) {
	carbs := []domain.CarbEvent{}
	insulin := []domain.InsulinEvent{}
	for _, row := range rows {
		ts, ok := extractTime(row)
		if !ok {
			continue
		}
		if rawCarbs, ok := row["carbs"]; ok {
			if grams, ok := toFloat(rawCarbs); ok && grams > 0 {
				carbs = append(carbs, domain.CarbEvent{TS: ts, Grams: grams})
			}
		}
		if rawIns, ok := row["insulin"]; ok {
			if units, ok := toFloat(rawIns); ok && units > 0 {
				insulin = append(insulin, domain.InsulinEvent{TS: ts, Units: units, Kind: "bolus"})
			}
		}
	}
	return carbs, insulin
}

func extractTime(row map[string]any) (time.Time, bool) {
	if raw, ok := row["dateString"]; ok {
		if s, ok := raw.(string); ok {
			ts, err := time.Parse(time.RFC3339, strings.ReplaceAll(s, "Z", "+00:00"))
			if err == nil {
				return ts, true
			}
		}
	}
	if raw, ok := row["created_at"]; ok {
		if s, ok := raw.(string); ok {
			ts, err := time.Parse(time.RFC3339, strings.ReplaceAll(s, "Z", "+00:00"))
			if err == nil {
				return ts, true
			}
		}
	}
	if raw, ok := row["date"]; ok {
		if ms, ok := toFloat(raw); ok {
			sec := int64(ms / 1000)
			nsec := int64(ms*1e6) - sec*1e9
			return time.Unix(sec, nsec).UTC(), true
		}
	}
	return time.Time{}, false
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		n, err := t.Float64()
		if err == nil {
			return n, true
		}
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err == nil {
			return n, true
		}
	}
	return 0, false
}

func clamp(v, minV, maxV float64) float64 {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}
