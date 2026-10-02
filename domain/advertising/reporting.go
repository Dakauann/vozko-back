package advertising

import (
	"errors"
	"slices"
	"sort"
)

var ErrBreakdownCombination = errors.New("meta does not allow this breakdown combination")

type Breakdown string

const (
	BreakdownAge       Breakdown = "age"
	BreakdownGender    Breakdown = "gender"
	BreakdownCountry   Breakdown = "country"
	BreakdownRegion    Breakdown = "region"
	BreakdownPlatform  Breakdown = "publisher_platform"
	BreakdownPosition  Breakdown = "platform_position"
	BreakdownDeviceOS  Breakdown = "device_platform"
	BreakdownHourOfDay Breakdown = "hourly_stats_aggregated_by_advertiser_time_zone"
)

var breakdownGroups = [][]Breakdown{
	{BreakdownAge},
	{BreakdownGender},
	{BreakdownAge, BreakdownGender},
	{BreakdownCountry},
	{BreakdownRegion},
	{BreakdownPlatform},
	{BreakdownPlatform, BreakdownPosition},
	{BreakdownDeviceOS},
	{BreakdownHourOfDay},
}

func BreakdownGroups() [][]Breakdown {
	out := make([][]Breakdown, len(breakdownGroups))
	for i, g := range breakdownGroups {
		out[i] = append([]Breakdown(nil), g...)
	}
	return out
}

func ValidateBreakdowns(b []Breakdown) error {
	if len(b) == 0 {
		return nil
	}
	sorted := append([]Breakdown(nil), b...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	for _, g := range breakdownGroups {
		candidate := append([]Breakdown(nil), g...)
		sort.Slice(candidate, func(i, j int) bool { return candidate[i] < candidate[j] })
		if slices.Equal(sorted, candidate) {
			return nil
		}
	}
	return ErrBreakdownCombination
}

type AttributionWindow string

const (
	Window1DayView   AttributionWindow = "1d_view"
	Window1DayClick  AttributionWindow = "1d_click"
	Window7DayClick  AttributionWindow = "7d_click"
	Window28DayClick AttributionWindow = "28d_click"
)

func AttributionWindows() []AttributionWindow {
	return []AttributionWindow{Window1DayView, Window1DayClick, Window7DayClick, Window28DayClick}
}

func ValidateWindows(w []AttributionWindow) error {
	for _, window := range w {
		if !slices.Contains(AttributionWindows(), window) {
			return FieldError("attributionWindows", "invalid")
		}
	}
	return nil
}

type VideoMetrics struct {
	Plays           int64   `json:"plays"`
	P25             int64   `json:"p25"`
	P50             int64   `json:"p50"`
	P75             int64   `json:"p75"`
	P95             int64   `json:"p95"`
	P100            int64   `json:"p100"`
	ThruPlays       int64   `json:"thruPlays"`
	AvgWatchSeconds float64 `json:"avgWatchSeconds"`
}

type LiveMetrics struct {
	Metrics
	Reach     int64
	Frequency float64
	Video     VideoMetrics
}

func (m LiveMetrics) CostPerThruPlay() *int64 { return ratio(m.SpendMicros, m.Video.ThruPlays) }

type LiveRow struct {
	ObjectID   string
	Dimensions map[Breakdown]string
	Values     LiveMetrics
}

type LiveQuery struct {
	WorkspaceID string
	AccountID   string
	Level       Level
	ObjectIDs   []string
	Range       DateRange
	Breakdowns  []Breakdown
	Windows     []AttributionWindow
}

func Typed[T ~string](raw []string) []T {
	out := make([]T, 0, len(raw))
	for _, v := range raw {
		out = append(out, T(v))
	}
	return out
}

func (q LiveQuery) Validate() error {
	if !q.Level.Valid() {
		return FieldError("level", "invalid")
	}
	if err := q.Range.Validate(); err != nil {
		return err
	}
	if err := ValidateBreakdowns(q.Breakdowns); err != nil {
		return err
	}
	return ValidateWindows(q.Windows)
}

func PreviousRange(r DateRange) DateRange {
	days := r.Days()
	until := r.Since.AddDate(0, 0, -1)
	return DateRange{Since: until.AddDate(0, 0, -(days - 1)), Until: until}
}

func PercentChange(current, previous float64) *float64 {
	if previous == 0 {
		return nil
	}
	change := (current - previous) / previous * 100
	return &change
}

func (q LiveQuery) UniqueFieldsAllowed() bool {
	return !slices.Contains(q.Breakdowns, BreakdownHourOfDay)
}
