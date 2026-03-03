package engine

import (
	"hash/fnv"
	"math"
	"math/rand"
)

type simulationOutcome struct {
	BenefitProb  float64
	RiskProb     float64
	ExpectedGain float64
	Samples      int
}

func runSimulation(seed int64, samples int, draw func(r *rand.Rand) float64, eval func(sample float64) (benefit bool, risk bool, gain float64)) simulationOutcome {
	if samples < 50 {
		samples = 50
	}
	rng := rand.New(rand.NewSource(seed))
	benefitHits := 0
	riskHits := 0
	totalGain := 0.0

	for i := 0; i < samples; i++ {
		sample := draw(rng)
		benefit, risk, gain := eval(sample)
		if benefit {
			benefitHits++
		}
		if risk {
			riskHits++
		}
		totalGain += gain
	}

	return simulationOutcome{
		BenefitProb:  float64(benefitHits) / float64(samples),
		RiskProb:     float64(riskHits) / float64(samples),
		ExpectedGain: totalGain / float64(samples),
		Samples:      samples,
	}
}

func normalSample(rng *rand.Rand, mean, std float64) float64 {
	if std <= 1e-9 {
		return mean
	}
	u1 := rng.Float64()
	if u1 < 1e-12 {
		u1 = 1e-12
	}
	u2 := rng.Float64()
	z := math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
	return mean + z*std
}

func standardError(variability *float64, samples int, fallback float64) float64 {
	if samples <= 1 {
		return fallback
	}
	if variability == nil || *variability <= 0 {
		return fallback
	}
	return math.Max(*variability/math.Sqrt(float64(samples)), fallback*0.5)
}

func stableSeed(parts ...string) int64 {
	h := fnv.New64a()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return int64(h.Sum64() & 0x7fffffffffffffff)
}
