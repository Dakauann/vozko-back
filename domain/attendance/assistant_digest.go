package attendance

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	DayLayout                 = "2006-01-02"
	MaxAssistantRangeDays     = 366
	DefaultAssistantRangeDays = 30
	MaxAssistantListItems     = 10
	DefaultAssistantListItems = 5
)

var (
	ErrInvalidWindow = errors.New("attendance: invalid date window")
	ErrUnknownMetric = errors.New("attendance: unknown metric")
)

func EndOfDay(day time.Time) time.Time {
	return day.Add(24*time.Hour - time.Second)
}

type Window struct {
	From time.Time
	To   time.Time
}

func ParseWindow(from, to string, today time.Time) (Window, error) {
	now := today.UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	end := todayStart
	if strings.TrimSpace(to) != "" {
		parsed, err := ParseDay(to)
		if err != nil {
			return Window{}, err
		}
		end = parsed
	}
	start := end.AddDate(0, 0, -(DefaultAssistantRangeDays - 1))
	if strings.TrimSpace(from) != "" {
		parsed, err := ParseDay(from)
		if err != nil {
			return Window{}, err
		}
		start = parsed
	}

	switch {
	case start.After(end):
		return Window{}, fmt.Errorf("%w: start %s is after end %s", ErrInvalidWindow, start.Format(DayLayout), end.Format(DayLayout))
	case start.After(todayStart):
		return Window{}, fmt.Errorf("%w: start %s is in the future", ErrInvalidWindow, start.Format(DayLayout))
	}
	w := Window{From: start, To: EndOfDay(end)}
	if w.Days() > MaxAssistantRangeDays {
		return Window{}, fmt.Errorf("%w: %d days exceeds the %d-day limit, split the period", ErrInvalidWindow, w.Days(), MaxAssistantRangeDays)
	}
	return w, nil
}

func ParseDay(raw string) (time.Time, error) {
	t, err := time.Parse(DayLayout, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %q is not a YYYY-MM-DD date", ErrInvalidWindow, raw)
	}
	return t, nil
}

func (w Window) Days() int {
	return int(w.To.Sub(w.From)/(24*time.Hour)) + 1
}

func (w Window) Apply(f *OverviewFilter) {
	from, to := w.From, w.To
	f.DateFrom = &from
	f.DateTo = &to
}

func MetricKeys() []string {
	keys := make([]string, 0, len(metricRegistry))
	for key := range metricRegistry {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func resolveMetricKeys(keys []string, allowEmpty bool) ([]string, error) {
	if len(keys) == 0 {
		if allowEmpty {
			return MetricKeys(), nil
		}
		return nil, fmt.Errorf("%w: name at least one of %s", ErrUnknownMetric, strings.Join(MetricKeys(), ", "))
	}
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, found := Metric(key); !found {
			return nil, fmt.Errorf("%w: %q, valid metrics are %s", ErrUnknownMetric, key, strings.Join(MetricKeys(), ", "))
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out, nil
}

type MetricReading struct {
	Key       string          `json:"key"`
	Kind      MetricKind      `json:"kind"`
	Direction MetricDirection `json:"direction"`
	Value     *float64        `json:"value"`
	Target    *float64        `json:"target,omitempty"`
	Projected *float64        `json:"projected,omitempty"`
	Verdict   Verdict         `json:"verdict,omitempty"`
}

func ReadMetrics(s *SummarySection, keys []string) ([]MetricReading, error) {
	resolved, err := resolveMetricKeys(keys, true)
	if err != nil {
		return nil, err
	}
	projections := make(map[string]MetricProjection)
	if s != nil {
		for _, p := range s.Projections {
			projections[p.MetricKey] = p
		}
	}
	out := make([]MetricReading, 0, len(resolved))
	for _, key := range resolved {
		spec, _ := Metric(key)
		reading := MetricReading{Key: key, Kind: spec.Kind, Direction: spec.Direction}
		if v, ok := MetricActual(s, key); ok && isFinite(v) {
			reading.Value = round2Ptr(v)
		}
		if p, ok := projections[key]; ok && p.Target != nil {
			reading.Target = p.Target
			reading.Projected = p.Projected
			reading.Verdict = p.Verdict
		}
		out = append(out, reading)
	}
	return out, nil
}

type MemberDigest struct {
	MemberID        string      `json:"member_id"`
	Name            string      `json:"name"`
	Kind            string      `json:"kind"`
	Value           float64     `json:"value"`
	PctOfTeamAvg    *float64    `json:"pct_of_team_avg,omitempty"`
	Resolved        int64       `json:"resolved"`
	Open            int64       `json:"open"`
	Pending         int64       `json:"pending"`
	ResolutionPct   float64     `json:"resolution_pct"`
	AvgResponseMins *float64    `json:"avg_response_mins,omitempty"`
	Rating          *float64    `json:"rating,omitempty"`
	Class           MemberClass `json:"class,omitempty"`
}

type TeamDigest struct {
	RankMetric   string         `json:"rank_metric"`
	TeamAverage  *float64       `json:"team_average,omitempty"`
	TotalMembers int            `json:"total_members"`
	Order        string         `json:"order"`
	Members      []MemberDigest `json:"members"`
	Available    bool           `json:"available"`
	Reason       string         `json:"reason,omitempty"`
}

func ClampListItems(requested int) int {
	switch {
	case requested <= 0:
		return DefaultAssistantListItems
	case requested > MaxAssistantListItems:
		return MaxAssistantListItems
	}
	return requested
}

func DigestTeam(r TeamRanking, limit int, bottom bool) TeamDigest {
	limit = ClampListItems(limit)
	out := TeamDigest{
		RankMetric:   r.RankMetricKey,
		TeamAverage:  r.TeamAverage,
		TotalMembers: len(r.Members),
		Order:        "top",
		Members:      []MemberDigest{},
		Available:    r.Available,
		Reason:       r.Reason,
	}
	if bottom {
		out.Order = "bottom"
	}
	for i := 0; i < len(r.Members) && len(out.Members) < limit; i++ {
		idx := i
		if bottom {
			idx = len(r.Members) - 1 - i
		}
		out.Members = append(out.Members, digestMember(r.Members[idx]))
	}
	return out
}

func digestMember(m RankedMember) MemberDigest {
	return MemberDigest{
		MemberID:        m.ActorID,
		Name:            m.DisplayName,
		Kind:            m.ActorKind,
		Value:           round2(m.RankMetricValue),
		PctOfTeamAvg:    m.PctOfTeamAvg,
		Resolved:        m.Resolved,
		Open:            m.Open,
		Pending:         m.Pending,
		ResolutionPct:   round2(m.ResolutionPct),
		AvgResponseMins: m.AvgResponseMins,
		Rating:          m.Rating,
		Class:           m.Class,
	}
}

type TrendPointDigest struct {
	Month   string  `json:"month"`
	Value   float64 `json:"value"`
	Partial bool    `json:"partial,omitempty"`
}

type TrendDigest struct {
	Metric    string             `json:"metric"`
	Kind      MetricKind         `json:"kind"`
	Direction MetricDirection    `json:"direction"`
	Points    []TrendPointDigest `json:"points"`
	BestMonth string             `json:"best_month,omitempty"`
	DeltaPct  *float64           `json:"delta_pct_vs_previous_month,omitempty"`
	Available bool               `json:"available"`
	Reason    string             `json:"reason,omitempty"`
}

func DigestTrend(t Trend, keys []string) ([]TrendDigest, error) {
	resolved, err := resolveMetricKeys(keys, false)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]TrendSeries, len(t.Series))
	for _, s := range t.Series {
		byKey[s.MetricKey] = s
	}
	out := make([]TrendDigest, 0, len(resolved))
	for _, key := range resolved {
		spec, _ := Metric(key)
		series, found := byKey[key]
		d := TrendDigest{Metric: key, Kind: spec.Kind, Direction: spec.Direction, Points: []TrendPointDigest{}}
		if !found {
			d.Reason = t.Reason
			out = append(out, d)
			continue
		}
		d.Available, d.Reason, d.BestMonth, d.DeltaPct = series.Available, series.Reason, series.BestBucket, series.DeltaPct
		for _, p := range series.Points {
			d.Points = append(d.Points, TrendPointDigest{Month: p.Bucket, Value: round2(p.Value), Partial: p.Partial})
		}
		out = append(out, d)
	}
	return out, nil
}

type BacklogDimensionDigest struct {
	Dimension string       `json:"dimension"`
	Buckets   []XrayBucket `json:"buckets"`
	Unknown   int64        `json:"unknown,omitempty"`
}

type BacklogDigest struct {
	Total      int64                    `json:"total"`
	Dimensions []BacklogDimensionDigest `json:"dimensions"`
	Available  bool                     `json:"available"`
	Reason     string                   `json:"reason,omitempty"`
}

func DigestBacklog(x BacklogXray) BacklogDigest {
	out := BacklogDigest{Total: x.Total, Dimensions: []BacklogDimensionDigest{}, Available: x.Available, Reason: x.Reason}
	for _, dim := range []XrayDimension{x.Age, x.Origin, x.Assignee, x.Tenure, x.Returning} {
		if !dim.Available {
			continue
		}
		out.Dimensions = append(out.Dimensions, BacklogDimensionDigest{Dimension: dim.Dimension, Buckets: dim.Buckets, Unknown: dim.Unknown})
	}
	return out
}

func (w Window) Previous() Window {
	days := w.Days()
	from := w.From.AddDate(0, 0, -days)
	return Window{From: from, To: w.From.Add(-time.Second)}
}

type Change string

const (
	ChangeImproved  Change = "improved"
	ChangeWorsened  Change = "worsened"
	ChangeUnchanged Change = "unchanged"
	ChangeUnknown   Change = "unknown"
)

type MetricComparison struct {
	MetricReading
	Previous *float64 `json:"previous"`
	Delta    *float64 `json:"delta"`
	DeltaPct *float64 `json:"delta_pct"`
	Change   Change   `json:"change"`
}

func CompareReadings(current, previous []MetricReading) []MetricComparison {
	before := make(map[string]*float64, len(previous))
	for _, p := range previous {
		before[p.Key] = p.Value
	}
	out := make([]MetricComparison, 0, len(current))
	for _, c := range current {
		cmp := MetricComparison{MetricReading: c, Previous: before[c.Key], Change: ChangeUnknown}
		if c.Value != nil && cmp.Previous != nil {
			delta := *c.Value - *cmp.Previous
			cmp.Delta = round2Ptr(delta)
			if pct, ok := ratioPct(delta, math.Abs(*cmp.Previous)); ok {
				cmp.DeltaPct = &pct
			}
			cmp.Change = directionOf(delta, c.Direction)
		}
		out = append(out, cmp)
	}
	return out
}

func directionOf(delta float64, direction MetricDirection) Change {
	switch {
	case delta == 0:
		return ChangeUnchanged
	case (delta > 0) == (direction == DirectionLowerIsBetter):
		return ChangeWorsened
	}
	return ChangeImproved
}
