package smartcampaign

import "math"

const oneSidedZ95 = 1.645

func lowerBound(hits, total int) float64 {
	if total == 0 {
		return 0
	}
	n := float64(total)
	share := float64(hits) / n
	z2 := oneSidedZ95 * oneSidedZ95
	centre := share + z2/(2*n)
	margin := oneSidedZ95 * math.Sqrt(share*(1-share)/n+z2/(4*n*n))
	return (centre - margin) / (1 + z2/n)
}
