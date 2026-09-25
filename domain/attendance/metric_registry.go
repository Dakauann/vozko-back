package attendance

import "sort"

type MetricKind string

const (
	MetricKindCount   MetricKind = "count"
	MetricKindPercent MetricKind = "percent"
	MetricKindMinutes MetricKind = "minutes"
	MetricKindMoney   MetricKind = "money"
)

func (k MetricKind) Valid() bool {
	switch k {
	case MetricKindCount, MetricKindPercent, MetricKindMinutes, MetricKindMoney:
		return true
	}
	return false
}

type MetricDirection string

const (
	DirectionHigherIsBetter MetricDirection = "higher"
	DirectionLowerIsBetter  MetricDirection = "lower"
)

type MetricCategory string

const (
	CategoryVolume  MetricCategory = "volume"
	CategoryTiming  MetricCategory = "timing"
	CategoryQuality MetricCategory = "quality"
	CategoryRevenue MetricCategory = "revenue"
)

var categoryOrder = map[MetricCategory]int{
	CategoryVolume:  0,
	CategoryTiming:  1,
	CategoryQuality: 2,
	CategoryRevenue: 3,
}

func (c MetricCategory) Order() int {
	if order, found := categoryOrder[c]; found {
		return order
	}
	return len(categoryOrder)
}

type MetricSpec struct {
	Key        string          `json:"key"`
	Kind       MetricKind      `json:"kind"`
	Direction  MetricDirection `json:"direction"`
	Cumulative bool            `json:"cumulative"`
	Targetable bool            `json:"targetable"`
	Category   MetricCategory  `json:"category"`
}

const (
	MetricFinished          = "finished"
	MetricEngaged           = "engaged"
	MetricEntriesCreated    = "entries_created"
	MetricNewLeads          = "new_leads"
	MetricWonCount          = "won_count"
	MetricRevenueCents      = "revenue_cents"
	MetricResolutionPct     = "resolution_pct"
	MetricAIContainmentRate = "ai_containment_rate"
	MetricReopenRate        = "reopen_rate"
	MetricOutcomeDurablePct = "outcome_durable_pct"
	MetricAvgFRTMins        = "avg_frt_mins"
	MetricAvgWaitMins       = "avg_wait_mins"
	MetricAvgHandleMins     = "avg_handle_mins"
)

var metricRegistry = map[string]MetricSpec{
	MetricFinished:          {Key: MetricFinished, Kind: MetricKindCount, Direction: DirectionHigherIsBetter, Cumulative: true, Targetable: true, Category: CategoryVolume},
	MetricEngaged:           {Key: MetricEngaged, Kind: MetricKindCount, Direction: DirectionHigherIsBetter, Cumulative: true, Targetable: true, Category: CategoryVolume},
	MetricEntriesCreated:    {Key: MetricEntriesCreated, Kind: MetricKindCount, Direction: DirectionHigherIsBetter, Cumulative: true, Targetable: true, Category: CategoryVolume},
	MetricNewLeads:          {Key: MetricNewLeads, Kind: MetricKindCount, Direction: DirectionHigherIsBetter, Cumulative: true, Targetable: true, Category: CategoryVolume},
	MetricWonCount:          {Key: MetricWonCount, Kind: MetricKindCount, Direction: DirectionHigherIsBetter, Cumulative: true, Targetable: true, Category: CategoryRevenue},
	MetricRevenueCents:      {Key: MetricRevenueCents, Kind: MetricKindMoney, Direction: DirectionHigherIsBetter, Cumulative: true, Targetable: true, Category: CategoryRevenue},
	MetricResolutionPct:     {Key: MetricResolutionPct, Kind: MetricKindPercent, Direction: DirectionHigherIsBetter, Cumulative: false, Targetable: true, Category: CategoryQuality},
	MetricAIContainmentRate: {Key: MetricAIContainmentRate, Kind: MetricKindPercent, Direction: DirectionHigherIsBetter, Cumulative: false, Targetable: true, Category: CategoryQuality},
	MetricReopenRate:        {Key: MetricReopenRate, Kind: MetricKindPercent, Direction: DirectionLowerIsBetter, Cumulative: false, Targetable: true, Category: CategoryQuality},
	MetricOutcomeDurablePct: {Key: MetricOutcomeDurablePct, Kind: MetricKindPercent, Direction: DirectionHigherIsBetter, Cumulative: false, Targetable: true, Category: CategoryQuality},
	MetricAvgFRTMins:        {Key: MetricAvgFRTMins, Kind: MetricKindMinutes, Direction: DirectionLowerIsBetter, Cumulative: false, Targetable: true, Category: CategoryTiming},
	MetricAvgWaitMins:       {Key: MetricAvgWaitMins, Kind: MetricKindMinutes, Direction: DirectionLowerIsBetter, Cumulative: false, Targetable: true, Category: CategoryTiming},
	MetricAvgHandleMins:     {Key: MetricAvgHandleMins, Kind: MetricKindMinutes, Direction: DirectionLowerIsBetter, Cumulative: false, Targetable: true, Category: CategoryTiming},
}

func Metric(key string) (MetricSpec, bool) {
	spec, found := metricRegistry[key]
	return spec, found
}

func TargetableMetrics() []MetricSpec {
	out := make([]MetricSpec, 0, len(metricRegistry))
	for _, spec := range metricRegistry {
		if spec.Targetable {
			out = append(out, spec)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category.Order() < out[j].Category.Order()
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func MetricActual(o *SummarySection, key string) (float64, bool) {
	if o == nil {
		return 0, false
	}
	switch key {
	case MetricFinished:
		return float64(o.KPIs.Finished), true
	case MetricEngaged:
		return float64(o.KPIs.Engaged), true
	case MetricEntriesCreated:
		return float64(o.KPIs.EntriesCreated), true
	case MetricNewLeads:
		return float64(o.KPIs.NewLeads), true
	case MetricWonCount:
		if !o.Revenue.Available {
			return 0, false
		}
		return float64(o.Revenue.WonCount()), true
	case MetricRevenueCents:
		if !o.Revenue.Available || o.Revenue.MixedCurrencies {
			return 0, false
		}
		return float64(o.Revenue.SingleCurrencyCents()), true
	case MetricResolutionPct:
		closed := o.KPIs.Finished + o.KPIs.Ongoing + o.KPIs.Pending
		if closed == 0 {
			return 0, false
		}
		return float64(o.KPIs.Finished) / float64(closed) * 100, true
	case MetricAIContainmentRate:
		if !o.AI.Available {
			return 0, false
		}
		return o.AI.ContainmentRate, true
	case MetricReopenRate:
		if !o.Reopen.Available || o.Reopen.ReopenRate == nil {
			return 0, false
		}
		return *o.Reopen.ReopenRate, true
	case MetricOutcomeDurablePct:
		if !o.Quality.Available || o.Quality.Team.DurablePct == nil {
			return 0, false
		}
		return *o.Quality.Team.DurablePct, true
	case MetricAvgFRTMins:
		if o.KPIs.AvgFRTMins == nil {
			return 0, false
		}
		return *o.KPIs.AvgFRTMins, true
	case MetricAvgWaitMins:
		if o.KPIs.AvgWaitMins == nil {
			return 0, false
		}
		return *o.KPIs.AvgWaitMins, true
	case MetricAvgHandleMins:
		if o.KPIs.AvgHandleMins == nil {
			return 0, false
		}
		return *o.KPIs.AvgHandleMins, true
	}
	return 0, false
}
