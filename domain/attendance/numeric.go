package attendance

import "math"

func round2(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*100) / 100
}

func round2Ptr(v float64) *float64 {
	r := round2(v)
	return &r
}

func ratioPct(part, whole float64) (float64, bool) {
	if whole == 0 || math.IsNaN(whole) || math.IsInf(whole, 0) {
		return 0, false
	}
	value := part / whole * 100
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return round2(value), true
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
