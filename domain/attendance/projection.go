package attendance

import (
	"time"

	"vozko/domain/working_hours"
)

type Verdict string

const (
	VerdictOnTrack      Verdict = "on_track"
	VerdictAtRisk       Verdict = "at_risk"
	VerdictOffTrack     Verdict = "off_track"
	VerdictNoTarget     Verdict = "no_target"
	VerdictNotProjected Verdict = "not_projected"

	VerdictInsufficientData Verdict = "insufficient_data"
)

const (
	ReasonNoSchedule        = "no_schedule"
	ReasonScheduleNeverOpen = "schedule_never_opens"
	ReasonRangeTooWide      = "range_too_wide"
	ReasonPeriodUnavailable = "period_unavailable"
	ReasonTooEarly          = "too_early_to_project"
	ReasonNotCumulative     = "metric_is_not_cumulative"
	ReasonNoActual          = "actual_unavailable"
	ReasonUnknownMetric     = "unknown_metric"
)

const (
	MinElapsedPctForProjection = 10.0
	MinOpenDaysForProjection   = 1
	AtRiskBandPct              = 5.0
)

type Period struct {
	Start         time.Time `json:"start"`
	End           time.Time `json:"end"`
	Timezone      string    `json:"timezone"`
	OpenDaysTotal int       `json:"open_days_total"`
	OpenDaysDone  int       `json:"open_days_done"`
	OpenDaysLeft  int       `json:"open_days_left"`
	OpenMinutes   int       `json:"open_minutes"`
	ElapsedPct    float64   `json:"elapsed_pct"`
	Available     bool      `json:"available"`
	Reason        string    `json:"reason,omitempty"`
}

type MetricProjection struct {
	MetricKey  string          `json:"metric_key"`
	Kind       MetricKind      `json:"kind"`
	Direction  MetricDirection `json:"direction"`
	Cumulative bool            `json:"cumulative"`
	Actual     float64         `json:"actual"`
	PerOpenDay *float64        `json:"per_open_day"`
	Projected  *float64        `json:"projected"`
	Target     *float64        `json:"target"`
	AttainPct  *float64        `json:"attain_pct"`
	Verdict    Verdict         `json:"verdict"`
	Available  bool            `json:"available"`
	Reason     string          `json:"reason,omitempty"`
}

func MonthRange(at time.Time, loc *time.Location) (time.Time, time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	local := at.In(loc)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	return start, start.AddDate(0, 1, 0)
}

func BuildPeriod(sched *working_hours.Schedule, start, end, now time.Time) Period {
	out := Period{Start: start, End: end, Timezone: time.UTC.String()}
	if sched == nil {
		out.Reason = ReasonNoSchedule
		return out
	}
	out.Timezone = sched.Location().String()

	elapsedMins, totalMins, ok := sched.Progress(start, end, now)
	if !ok {
		if _, spanOK := sched.OpenMinutes(start, end); !spanOK {
			out.Reason = ReasonRangeTooWide
			return out
		}
		out.Reason = ReasonScheduleNeverOpen
		return out
	}

	totalDays, ok := sched.OpenDays(start, end)
	if !ok {
		out.Reason = ReasonRangeTooWide
		return out
	}

	cursor := now
	if cursor.Before(start) {
		cursor = start
	}
	if cursor.After(end) {
		cursor = end
	}
	doneDays, ok := sched.OpenDays(start, cursor)
	if !ok {
		out.Reason = ReasonRangeTooWide
		return out
	}
	if doneDays > totalDays {
		doneDays = totalDays
	}

	pct, _ := ratioPct(float64(elapsedMins), float64(totalMins))

	out.OpenMinutes = totalMins
	out.OpenDaysTotal = totalDays
	out.OpenDaysDone = doneDays
	out.OpenDaysLeft = totalDays - doneDays
	out.ElapsedPct = pct
	out.Available = true
	return out
}

func BuildProjection(spec MetricSpec, actual float64, actualKnown bool, target *float64, p Period) MetricProjection {
	out := MetricProjection{
		MetricKey:  spec.Key,
		Kind:       spec.Kind,
		Direction:  spec.Direction,
		Cumulative: spec.Cumulative,
		Verdict:    VerdictNoTarget,
	}
	if !actualKnown || !isFinite(actual) {
		out.Reason = ReasonNoActual
		return out
	}
	out.Actual = round2(actual)
	out.Available = true

	if target != nil && isFinite(*target) {
		value := round2(*target)
		out.Target = &value
	}

	if !spec.Cumulative {
		out.Verdict = VerdictNotProjected
		out.Reason = ReasonNotCumulative
		if out.Target != nil {
			out.AttainPct = attainment(spec.Direction, out.Actual, *out.Target)
			out.Verdict = verdictFor(spec.Direction, out.Actual, *out.Target)
		}
		return out
	}

	if !p.Available {
		out.Verdict = VerdictNotProjected
		out.Reason = ReasonPeriodUnavailable
		if p.Reason != "" {
			out.Reason = p.Reason
		}
		return out
	}
	if p.OpenDaysDone < MinOpenDaysForProjection || p.ElapsedPct < MinElapsedPctForProjection {
		out.Verdict = VerdictNotProjected
		out.Reason = ReasonTooEarly
		return out
	}

	perDay := out.Actual / float64(p.OpenDaysDone)
	projected := perDay * float64(p.OpenDaysTotal)
	if !isFinite(perDay) || !isFinite(projected) {
		out.Verdict = VerdictNotProjected
		out.Reason = ReasonTooEarly
		return out
	}
	out.PerOpenDay = round2Ptr(perDay)
	out.Projected = round2Ptr(projected)

	if out.Target == nil {
		out.Verdict = VerdictNoTarget
		return out
	}
	out.AttainPct = attainment(spec.Direction, *out.Projected, *out.Target)
	out.Verdict = verdictFor(spec.Direction, *out.Projected, *out.Target)
	return out
}

func attainment(direction MetricDirection, value, target float64) *float64 {
	if direction == DirectionLowerIsBetter {
		pct, ok := ratioPct(target, value)
		if !ok {
			return nil
		}
		return &pct
	}
	pct, ok := ratioPct(value, target)
	if !ok {
		return nil
	}
	return &pct
}

func verdictFor(direction MetricDirection, value, target float64) Verdict {
	band := target * AtRiskBandPct / 100
	if band < 0 {
		band = -band
	}
	if direction == DirectionLowerIsBetter {
		switch {
		case value <= target:
			return VerdictOnTrack
		case value <= target+band:
			return VerdictAtRisk
		default:
			return VerdictOffTrack
		}
	}
	switch {
	case value >= target:
		return VerdictOnTrack
	case value >= target-band:
		return VerdictAtRisk
	default:
		return VerdictOffTrack
	}
}
