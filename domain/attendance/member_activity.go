package attendance

import (
	"sort"
	"time"
)

const (
	FlagLateStart            = "late_start"
	FlagNoPresence           = "no_presence"
	FlagPossibleForgottenTab = "possible_forgotten_tab"

	LongSessionHours = 14

	lateStartTolerance   = time.Hour
	minDaysForUsualStart = 3
	minDaysForUsualDay   = 2
	TriggerRoundRobin    = "inbound_rr"
	activityDateLayout   = "2006-01-02"
	activityClockLayout  = "15:04"
	callJoinTolerance    = time.Minute
)

type PresenceSpan struct {
	Start  time.Time
	End    time.Time
	Open   bool
	OnCall bool
}

type Handout struct {
	At      time.Time
	Trigger string
}

type ActivityInput struct {
	Spans    []PresenceSpan
	Handouts []Handout
	From     time.Time
	To       time.Time
	Now      time.Time
	Location *time.Location
}

type ActivitySession struct {
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	OnCallMS int64     `json:"on_call_ms"`
	Open     bool      `json:"open"`
}

type ActivityDay struct {
	Date        string            `json:"date"`
	Weekday     int               `json:"weekday"`
	ConnectedMS int64             `json:"connected_ms"`
	OnCallMS    int64             `json:"on_call_ms"`
	Sessions    []ActivitySession `json:"sessions"`
	Flags       []string          `json:"flags"`
}

type MemberActivity struct {
	Timezone             string         `json:"timezone"`
	UsualStart           string         `json:"usual_start,omitempty"`
	ConnectedMS          int64          `json:"connected_ms"`
	OnCallMS             int64          `json:"on_call_ms"`
	Days                 []ActivityDay  `json:"days"`
	Heatmap              [7][24]int     `json:"heatmap_minutes"`
	Received             map[string]int `json:"received"`
	ReceivedWhileOffline int            `json:"received_while_offline"`
}

func BuildMemberActivity(in ActivityInput) MemberActivity {
	loc := in.Location
	if loc == nil {
		loc = time.UTC
	}
	spans := closeOpenSpans(in.Spans, in.Now)
	sessions := joinCalls(spans)
	out := MemberActivity{Timezone: loc.String(), Received: map[string]int{}}
	out.Days = dayFrame(in.From, in.To, loc)
	index := make(map[string]int, len(out.Days))
	for i, d := range out.Days {
		index[d.Date] = i
	}
	for _, s := range sessions {
		i, ok := index[s.Start.In(loc).Format(activityDateLayout)]
		if !ok {
			continue
		}
		d := &out.Days[i]
		d.Sessions = append(d.Sessions, s)
		d.ConnectedMS += s.End.Sub(s.Start).Milliseconds()
		d.OnCallMS += s.OnCallMS
		if s.End.Sub(s.Start) >= LongSessionHours*time.Hour {
			d.addFlag(FlagPossibleForgottenTab)
		}
	}
	for _, d := range out.Days {
		out.ConnectedMS += d.ConnectedMS
		out.OnCallMS += d.OnCallMS
	}
	usual, hasUsual := usualStart(out.Days, loc)
	if hasUsual {
		out.UsualStart = clock(usual)
	}
	flagDays(out.Days, usual, hasUsual, in.Now, loc)
	out.Heatmap = heatmap(spans, in.From, in.To, loc)
	for _, h := range in.Handouts {
		if h.At.Before(in.From) || h.At.After(in.To) {
			continue
		}
		out.Received[h.Trigger]++
		if h.Trigger == TriggerRoundRobin && !covered(spans, h.At) {
			out.ReceivedWhileOffline++
		}
	}
	return out
}

func (d *ActivityDay) addFlag(flag string) {
	for _, f := range d.Flags {
		if f == flag {
			return
		}
	}
	d.Flags = append(d.Flags, flag)
}

func closeOpenSpans(spans []PresenceSpan, now time.Time) []PresenceSpan {
	out := make([]PresenceSpan, 0, len(spans))
	for _, s := range spans {
		if s.Open || s.End.IsZero() {
			s.End, s.Open = now, true
		}
		if s.End.After(s.Start) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

func joinCalls(spans []PresenceSpan) []ActivitySession {
	var out []ActivitySession
	var previous PresenceSpan
	for i, s := range spans {
		callMS := int64(0)
		if s.OnCall {
			callMS = s.End.Sub(s.Start).Milliseconds()
		}
		touchesCall := i > 0 && (s.OnCall || previous.OnCall) && s.Start.Sub(out[len(out)-1].End) <= callJoinTolerance
		if touchesCall {
			last := &out[len(out)-1]
			if s.End.After(last.End) {
				last.End = s.End
			}
			last.OnCallMS += callMS
			last.Open = s.Open
		} else {
			out = append(out, ActivitySession{Start: s.Start, End: s.End, OnCallMS: callMS, Open: s.Open})
		}
		previous = s
	}
	return out
}

func dayFrame(from, to time.Time, loc *time.Location) []ActivityDay {
	var days []ActivityDay
	start := startOfDay(from.In(loc))
	for d := start; !d.After(to.In(loc)); d = d.AddDate(0, 0, 1) {
		days = append(days, ActivityDay{Date: d.Format(activityDateLayout), Weekday: int(d.Weekday())})
	}
	return days
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func minuteOfDay(t time.Time) int {
	return t.Hour()*60 + t.Minute()
}

func clock(minutes int) string {
	return time.Date(2000, 1, 1, minutes/60, minutes%60, 0, 0, time.UTC).Format(activityClockLayout)
}

func usualStart(days []ActivityDay, loc *time.Location) (int, bool) {
	var starts []int
	for _, d := range days {
		if len(d.Sessions) > 0 {
			starts = append(starts, minuteOfDay(d.Sessions[0].Start.In(loc)))
		}
	}
	if len(starts) < minDaysForUsualStart {
		return 0, false
	}
	sort.Ints(starts)
	return starts[(len(starts)-1)/2], true
}

func flagDays(days []ActivityDay, usual int, hasUsual bool, now time.Time, loc *time.Location) {
	worked, seen := map[int]int{}, map[int]int{}
	for _, d := range days {
		seen[d.Weekday]++
		if len(d.Sessions) > 0 {
			worked[d.Weekday]++
		}
	}
	today := now.In(loc).Format(activityDateLayout)
	nowMinute := minuteOfDay(now.In(loc))
	tolerance := int(lateStartTolerance.Minutes())
	for i := range days {
		d := &days[i]
		if d.Date > today {
			continue
		}
		if len(d.Sessions) > 0 {
			if hasUsual && minuteOfDay(d.Sessions[0].Start.In(loc)) > usual+tolerance {
				d.addFlag(FlagLateStart)
			}
			continue
		}
		usualDay := worked[d.Weekday] >= minDaysForUsualDay && worked[d.Weekday]*2 >= seen[d.Weekday]
		started := d.Date < today || (hasUsual && nowMinute > usual+tolerance)
		if usualDay && started {
			d.addFlag(FlagNoPresence)
		}
	}
}

func heatmap(spans []PresenceSpan, from, to time.Time, loc *time.Location) [7][24]int {
	var grid [7][24]int
	for _, s := range spans {
		start, end := s.Start, s.End
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		for cursor := start; cursor.Before(end); {
			local := cursor.In(loc)
			nextHour := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), 0, 0, 0, loc).Add(time.Hour)
			stop := end
			if nextHour.Before(stop) {
				stop = nextHour
			}
			grid[int(local.Weekday())][local.Hour()] += int(stop.Sub(cursor).Minutes())
			cursor = stop
		}
	}
	return grid
}

func covered(spans []PresenceSpan, at time.Time) bool {
	for _, s := range spans {
		if !at.Before(s.Start) && !at.After(s.End) {
			return true
		}
	}
	return false
}
