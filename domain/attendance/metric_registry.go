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

type MetricSpec struct {
	Key        string          `json:"key"`
	Kind       MetricKind      `json:"kind"`
	Direction  MetricDirection `json:"direction"`
	Cumulative bool            `json:"cumulative"`
	Targetable bool            `json:"targetable"`
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
	MetricFinished:          {Key: MetricFinished, Kind: MetricKindCount, Direction: DirectionHigherIsBetter, Cumulative: true, Targetable: true},
	MetricEngaged:           {Key: MetricEngaged, Kind: MetricKindCount, Direction: DirectionHigherIsBetter, Cumulative: true, Targetable: true},
	MetricEntriesCreated:    {Key: MetricEntriesCreated, Kind: MetricKindCount, Direction: DirectionHigherIsBetter, Cumulative: true, Targetable: true},
	MetricNewLeads:          {Key: MetricNewLeads, Kind: MetricKindCount, Direction: DirectionHigherIsBetter, Cumulative: true, Targetable: true},
	MetricWonCount:          {Key: MetricWonCount, Kind: MetricKindCount, Direction: DirectionHigherIsBetter, Cumulative: true, Targetable: true},
	MetricRevenueCents:      {Key: MetricRevenueCents, Kind: MetricKindMoney, Direction: DirectionHigherIsBetter, Cumulative: true, Targetable: true},
	MetricResolutionPct:     {Key: MetricResolutionPct, Kind: MetricKindPercent, Direction: DirectionHigherIsBetter, Cumulative: false, Targetable: true},
	MetricAIContainmentRate: {Key: MetricAIContainmentRate, Kind: MetricKindPercent, Direction: DirectionHigherIsBetter, Cumulative: false, Targetable: true},
	MetricReopenRate:        {Key: MetricReopenRate, Kind: MetricKindPercent, Direction: DirectionLowerIsBetter, Cumulative: false, Targetable: true},
	MetricOutcomeDurablePct: {Key: MetricOutcomeDurablePct, Kind: MetricKindPercent, Direction: DirectionHigherIsBetter, Cumulative: false, Targetable: true},
	MetricAvgFRTMins:        {Key: MetricAvgFRTMins, Kind: MetricKindMinutes, Direction: DirectionLowerIsBetter, Cumulative: false, Targetable: true},
	MetricAvgWaitMins:       {Key: MetricAvgWaitMins, Kind: MetricKindMinutes, Direction: DirectionLowerIsBetter, Cumulative: false, Targetable: true},
	MetricAvgHandleMins:     {Key: MetricAvgHandleMins, Kind: MetricKindMinutes, Direction: DirectionLowerIsBetter, Cumulative: false, Targetable: true},
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
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func MetricActual(o *Overview, key string) (float64, bool) {
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
