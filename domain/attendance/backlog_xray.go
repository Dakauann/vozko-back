package attendance

import "sort"

const (
	XrayOtherKey     = "_other"
	XrayUnknownKey   = "_unknown"
	XrayTopN         = 5
	ReasonNoCoverage = "no_coverage"
	ReasonNoBacklog  = "no_backlog"
	ReasonNoWindow   = "no_window_model"
)

const (
	XrayDimensionOrigin    = "origin"
	XrayDimensionAssignee  = "assignee"
	XrayDimensionAge       = "age"
	XrayDimensionTenure    = "tenure"
	XrayDimensionReturning = "returning"
)

type XrayTally struct {
	Key      string
	Label    string
	Count    int64
	Position int
}

type XrayBucket struct {
	Key   string  `json:"key"`
	Label string  `json:"label,omitempty"`
	Count int64   `json:"count"`
	Pct   float64 `json:"pct"`
}

type XrayDimension struct {
	Dimension string       `json:"dimension"`
	Buckets   []XrayBucket `json:"buckets"`
	Measured  int64        `json:"measured"`
	Unknown   int64        `json:"unknown"`
	Available bool         `json:"available"`
	Reason    string       `json:"reason,omitempty"`
}

type RecordFieldTally struct {
	Key    string
	Filled int64
}

type RecordField struct {
	Key    string  `json:"key"`
	Filled int64   `json:"filled"`
	Pct    float64 `json:"pct"`
}

type RecordCompleteness struct {
	Fields      []RecordField `json:"fields"`
	Measured    int64         `json:"measured"`
	FullyFilled int64         `json:"fully_filled"`
	AvgFillPct  *float64      `json:"avg_fill_pct"`
	Available   bool          `json:"available"`
	Reason      string        `json:"reason,omitempty"`
}

type Reachability struct {
	Channel      string  `json:"channel"`
	Measured     int64   `json:"measured"`
	WindowOpen   int64   `json:"window_open"`
	WindowClosed int64   `json:"window_closed"`
	ClosedPct    float64 `json:"closed_pct"`
	Available    bool    `json:"available"`
	Reason       string  `json:"reason,omitempty"`
}

type BacklogXray struct {
	Total              int64              `json:"total"`
	Origin             XrayDimension      `json:"origin"`
	Assignee           XrayDimension      `json:"assignee"`
	Age                XrayDimension      `json:"age"`
	Tenure             XrayDimension      `json:"tenure"`
	Returning          XrayDimension      `json:"returning"`
	RecordCompleteness RecordCompleteness `json:"record_completeness"`
	Reachability       []Reachability     `json:"reachability"`
	Available          bool               `json:"available"`
	Reason             string             `json:"reason,omitempty"`
}

func UnavailableXrayDimension(dimension, reason string) XrayDimension {
	return XrayDimension{Dimension: dimension, Buckets: []XrayBucket{}, Reason: reason}
}

func BuildRankedDimension(dimension string, tallies []XrayTally, unknown int64, topN int) XrayDimension {
	out := XrayDimension{Dimension: dimension, Buckets: []XrayBucket{}, Unknown: clampNonNegative(unknown)}
	for _, t := range tallies {
		out.Measured += clampNonNegative(t.Count)
	}
	if out.Measured == 0 {
		out.Reason = ReasonNoCoverage
		return out
	}

	ranked := make([]XrayTally, 0, len(tallies))
	for _, t := range tallies {
		if t.Count <= 0 {
			continue
		}
		ranked = append(ranked, t)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Count != ranked[j].Count {
			return ranked[i].Count > ranked[j].Count
		}
		return ranked[i].Key < ranked[j].Key
	})

	if topN <= 0 || topN > len(ranked) {
		topN = len(ranked)
	}
	var tail int64
	for i, t := range ranked {
		if i < topN {
			out.Buckets = append(out.Buckets, XrayBucket{
				Key:   t.Key,
				Label: t.Label,
				Count: t.Count,
				Pct:   sharePct(t.Count, out.Measured),
			})
			continue
		}
		tail += t.Count
	}
	if tail > 0 {
		out.Buckets = append(out.Buckets, XrayBucket{
			Key:   XrayOtherKey,
			Count: tail,
			Pct:   sharePct(tail, out.Measured),
		})
	}
	out.Available = true
	return out
}

func BuildOrderedDimension(dimension string, tallies []XrayTally, unknown int64) XrayDimension {
	out := XrayDimension{Dimension: dimension, Buckets: []XrayBucket{}, Unknown: clampNonNegative(unknown)}
	for _, t := range tallies {
		out.Measured += clampNonNegative(t.Count)
	}
	if out.Measured == 0 {
		out.Reason = ReasonNoCoverage
		return out
	}

	ordered := make([]XrayTally, len(tallies))
	copy(ordered, tallies)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Position < ordered[j].Position })

	for _, t := range ordered {
		out.Buckets = append(out.Buckets, XrayBucket{
			Key:   t.Key,
			Label: t.Label,
			Count: clampNonNegative(t.Count),
			Pct:   sharePct(t.Count, out.Measured),
		})
	}
	out.Available = true
	return out
}

func BuildRecordCompleteness(tallies []RecordFieldTally, measured, fullyFilled int64) RecordCompleteness {
	out := RecordCompleteness{
		Fields:      []RecordField{},
		Measured:    clampNonNegative(measured),
		FullyFilled: clampNonNegative(fullyFilled),
	}
	if len(tallies) == 0 {
		out.Reason = ReasonNoCoverage
		return out
	}
	if out.Measured == 0 {
		out.Reason = ReasonNoBacklog
		return out
	}

	var filledSum float64
	for _, tally := range tallies {
		filled := clampNonNegative(tally.Filled)
		if filled > out.Measured {
			filled = out.Measured
		}
		filledSum += float64(filled)
		out.Fields = append(out.Fields, RecordField{
			Key:    tally.Key,
			Filled: filled,
			Pct:    sharePct(filled, out.Measured),
		})
	}

	pct, ok := ratioPct(filledSum, float64(out.Measured)*float64(len(tallies)))
	if !ok {
		out.Reason = ReasonNoCoverage
		return out
	}
	out.AvgFillPct = &pct
	out.Available = true
	return out
}

func BuildReachability(channel string, measured, windowOpen int64, supported bool) Reachability {
	out := Reachability{Channel: channel, Measured: clampNonNegative(measured)}
	if !supported {
		out.Reason = ReasonNoWindow
		return out
	}
	if out.Measured == 0 {
		out.Reason = ReasonNoBacklog
		return out
	}
	out.WindowOpen = clampNonNegative(windowOpen)
	if out.WindowOpen > out.Measured {
		out.WindowOpen = out.Measured
	}
	out.WindowClosed = out.Measured - out.WindowOpen
	out.ClosedPct = sharePct(out.WindowClosed, out.Measured)
	out.Available = true
	return out
}
