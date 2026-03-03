package stats

import "sort"

func RobustMedian(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		v := sorted[mid]
		return &v
	}
	v := (sorted[mid-1] + sorted[mid]) / 2
	return &v
}

func MAD(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	med := RobustMedian(values)
	if med == nil {
		return nil
	}
	dev := make([]float64, 0, len(values))
	for _, v := range values {
		d := v - *med
		if d < 0 {
			d = -d
		}
		dev = append(dev, d)
	}
	return RobustMedian(dev)
}

func Winsorized(values []float64, zLimit float64) []float64 {
	if len(values) < 4 {
		out := make([]float64, len(values))
		copy(out, values)
		return out
	}
	med := RobustMedian(values)
	mad := MAD(values)
	if med == nil || mad == nil || *mad == 0 {
		out := make([]float64, len(values))
		copy(out, values)
		return out
	}
	sigma := *mad * 1.4826
	lower := *med - zLimit*sigma
	upper := *med + zLimit*sigma
	out := make([]float64, 0, len(values))
	for _, v := range values {
		if v < lower {
			v = lower
		}
		if v > upper {
			v = upper
		}
		out = append(out, v)
	}
	return out
}

func RobustMean(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	clipped := Winsorized(values, 3.5)
	sum := 0.0
	for _, v := range clipped {
		sum += v
	}
	m := sum / float64(len(clipped))
	return &m
}
