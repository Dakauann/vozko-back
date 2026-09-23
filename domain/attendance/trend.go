package attendance

import "sort"

const (
	MaxTrendBuckets     = 24
	DefaultTrendBuckets = 13
)

const TrendBucketLayout = "2006-01"

const (
	ReasonNoClosedBuckets  = "no_closed_buckets"
	ReasonTrendUnavailable = "trend_repository_unavailable"
)

func ClampTrendBuckets(requested int) int {
	if requested <= 0 {
		return DefaultTrendBuckets
	}
	if requested > MaxTrendBuckets {
		return MaxTrendBuckets
	}
	return requested
}

type TrendPoint struct {
	Bucket    string  `json:"bucket"`
	Value     float64 `json:"value"`
	Partial   bool    `json:"partial"`
	Projected bool    `json:"projected"`
}

type TrendSeries struct {
	MetricKey  string          `json:"metric_key"`
	Kind       MetricKind      `json:"kind"`
	Direction  MetricDirection `json:"direction"`
	Points     []TrendPoint    `json:"points"`
	BestBucket string          `json:"best_bucket,omitempty"`
	BestValue  *float64        `json:"best_value,omitempty"`
	WindowFrom string          `json:"window_from,omitempty"`
	WindowTo   string          `json:"window_to,omitempty"`
	PrevClosed *float64        `json:"prev_closed"`
	DeltaPct   *float64        `json:"delta_pct"`
	Available  bool            `json:"available"`
	Reason     string          `json:"reason,omitempty"`
}

type Trend struct {
	Series     []TrendSeries `json:"series"`
	Unbucketed int64         `json:"unbucketed"`
	Available  bool          `json:"available"`
	Reason     string        `json:"reason,omitempty"`
}

func UnavailableTrend(reason string) Trend {
	return Trend{Series: []TrendSeries{}, Reason: reason}
}

func BuildTrend(spec MetricSpec, points []TrendPoint, projected *float64) TrendSeries {
	out := TrendSeries{
		MetricKey: spec.Key,
		Kind:      spec.Kind,
		Direction: spec.Direction,
		Points:    []TrendPoint{},
	}

	ordered := make([]TrendPoint, len(points))
	copy(ordered, points)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Bucket < ordered[j].Bucket })

	var closed []TrendPoint
	var partial *TrendPoint
	for i := range ordered {
		ordered[i].Projected = false
		out.Points = append(out.Points, ordered[i])
		if ordered[i].Partial {
			partial = &ordered[i]
			continue
		}
		closed = append(closed, ordered[i])
	}

	if len(out.Points) > 0 {
		out.WindowFrom = out.Points[0].Bucket
		out.WindowTo = out.Points[len(out.Points)-1].Bucket
	}

	if projected != nil && partial != nil && isFinite(*projected) {
		out.Points = append(out.Points, TrendPoint{
			Bucket:    partial.Bucket,
			Value:     round2(*projected),
			Projected: true,
		})
	}

	if len(closed) == 0 {
		out.Reason = ReasonNoClosedBuckets
		return out
	}
	out.Available = true

	best := closed[0]
	for _, point := range closed[1:] {
		if spec.Direction == DirectionLowerIsBetter {
			if point.Value < best.Value {
				best = point
			}
			continue
		}
		if point.Value > best.Value {
			best = point
		}
	}
	bestValue := round2(best.Value)
	out.BestBucket = best.Bucket
	out.BestValue = &bestValue

	previous := round2(closed[len(closed)-1].Value)
	out.PrevClosed = &previous

	reference, hasReference := float64(0), false
	if projected != nil && isFinite(*projected) {
		reference, hasReference = *projected, true
	} else if partial != nil {
		reference, hasReference = partial.Value, true
	}
	if hasReference {
		if delta, ok := ratioPct(reference-previous, previous); ok {
			out.DeltaPct = &delta
		}
	}
	return out
}
