// Package working_hours models a weekly recurring open/closed schedule and the
// business time that accrues inside it.
//
// It exists for the assignment rescue sweep. A conversation handed to an agent
// at 17:55 must not be taken away at 18:10 because the office closed at 18:00,
// and it must not come back at 09:00 already fifteen hours overdue. Both follow
// from one primitive, Elapsed, which counts only the time the schedule was
// open — so a deadline measured with it is a deadline in working minutes.
//
// Nothing here reads the clock. Every entry point takes the instants it is
// asked about, which is what makes the whole package testable without a fake
// clock and safe to call from a job that already has one.
package working_hours

import (
	"errors"
	"sort"
	"time"
)

// MinutesPerDay is the span of one local day in minutes. An interval's end may
// exceed it (see Interval), a start may not.
const MinutesPerDay = 24 * 60

// nextOpenHorizonDays bounds the forward scan in NextOpen. A schedule that
// opens less often than once a fortnight is indistinguishable from a broken
// one, and an unbounded scan on a never-open schedule would not terminate.
const nextOpenHorizonDays = 14

var (
	ErrNoLocation         = errors.New("working hours: schedule has no timezone")
	ErrIntervalOutOfRange = errors.New("working hours: interval outside the supported range")
	ErrIntervalEmpty      = errors.New("working hours: interval ends at or before it starts")
	ErrIntervalOverlap    = errors.New("working hours: overlapping intervals on the same day")
	ErrNoOpenTime         = errors.New("working hours: schedule never opens")
)

// Interval is a half-open [StartMin, EndMin) range of minutes from local
// midnight.
//
// Half-open so that 09:00-18:00 and 18:00-22:00 describe adjacent windows
// rather than overlapping ones. EndMin may exceed MinutesPerDay to express a
// shift that runs past midnight into the following day: 22:00-02:00 is
// {StartMin: 1320, EndMin: 1560}. That keeps an overnight shift one interval on
// one weekday, instead of two halves an admin has to remember to edit together.
type Interval struct {
	StartMin int
	EndMin   int
}

// Schedule is a weekly recurring schedule evaluated in one timezone.
//
// A nil *Schedule means "no working hours configured", which is always open.
// That is the backward-compatible default and the reason every method has a nil
// receiver branch: a workspace that never sets hours keeps behaving exactly as
// it does today, with no migration and no opt-out flag.
type Schedule struct {
	loc  *time.Location
	days [7][]Interval
}

// New builds a schedule from per-weekday intervals. The intervals are copied
// and sorted by start, so the caller's slices are not retained or reordered and
// every later scan can assume order.
func New(loc *time.Location, days map[time.Weekday][]Interval) *Schedule {
	s := &Schedule{loc: loc}
	for wd, ivs := range days {
		if wd < time.Sunday || wd > time.Saturday {
			continue
		}
		cp := make([]Interval, len(ivs))
		copy(cp, ivs)
		sort.Slice(cp, func(i, j int) bool { return cp[i].StartMin < cp[j].StartMin })
		s.days[wd] = cp
	}
	return s
}

// Location is the timezone the schedule is evaluated in, UTC when unset.
func (s *Schedule) Location() *time.Location {
	if s == nil || s.loc == nil {
		return time.UTC
	}
	return s.loc
}

// IsOpen reports whether the schedule is open at that instant.
func (s *Schedule) IsOpen(at time.Time) bool {
	if s == nil {
		return true
	}
	local := at.In(s.Location())
	y, m, d := local.Date()
	// Yesterday first: a shift that started before midnight is still running.
	for _, offset := range [2]int{-1, 0} {
		for _, seg := range s.segmentsOn(y, m, d+offset) {
			if !local.Before(seg[0]) && local.Before(seg[1]) {
				return true
			}
		}
	}
	return false
}

// Elapsed is the open time between two instants — the whole point of the
// package.
//
// It is always <= to.Sub(from), which is what lets a caller keep using a
// wall-clock database filter as a cheap pre-select: anything whose business
// time exceeds a budget has necessarily exceeded it in wall time too, so the
// coarse filter can never hide a due conversation.
func (s *Schedule) Elapsed(from, to time.Time) time.Duration {
	if !to.After(from) {
		return 0
	}
	if s == nil {
		return to.Sub(from)
	}
	var total time.Duration
	for _, seg := range s.segmentsBetween(from, to) {
		total += overlap(seg[0], seg[1], from, to)
	}
	return total
}

// NextOpen returns the first instant at or after `at` when the schedule is
// open, reporting false when it does not open inside the search horizon.
//
// The sweep uses it for one thing: saying in a log line when a skipped
// workspace will be looked at again, so a quiet night is legible rather than
// indistinguishable from a broken job.
func (s *Schedule) NextOpen(at time.Time) (time.Time, bool) {
	if s == nil {
		return at, true
	}
	if s.IsOpen(at) {
		return at, true
	}
	local := at.In(s.Location())
	y, m, d := local.Date()
	for offset := 0; offset <= nextOpenHorizonDays; offset++ {
		for _, seg := range s.segmentsOn(y, m, d+offset) {
			if !seg[0].Before(local) {
				return seg[0], true
			}
		}
	}
	return time.Time{}, false
}

// Validate rejects the shapes that would misbehave silently rather than loudly.
//
// The one worth naming is ErrNoOpenTime: a schedule with no open minute anywhere
// in the week would freeze every deadline in that scope forever, and it looks
// identical to "not configured" in a UI that renders an empty week.
func (s *Schedule) Validate() error {
	if s == nil {
		return nil
	}
	if s.loc == nil {
		return ErrNoLocation
	}
	open := false
	for _, ivs := range s.days {
		for i, iv := range ivs {
			if iv.StartMin < 0 || iv.StartMin >= MinutesPerDay {
				return ErrIntervalOutOfRange
			}
			if iv.EndMin > 2*MinutesPerDay {
				return ErrIntervalOutOfRange
			}
			if iv.EndMin <= iv.StartMin {
				return ErrIntervalEmpty
			}
			if i > 0 && iv.StartMin < ivs[i-1].EndMin {
				return ErrIntervalOverlap
			}
			open = true
		}
	}
	if !open {
		return ErrNoOpenTime
	}
	return nil
}

// Resolve picks the schedule that governs an entry.
//
// A department's own hours win outright; a department without hours inherits
// the workspace's. Override rather than intersection, because a support desk
// that works Saturdays inside a Mon-Fri company is the case this exists for,
// and an intersection could not express it.
func Resolve(workspace, department *Schedule) *Schedule {
	if department != nil {
		return department
	}
	return workspace
}

// segmentsOn materializes one local day's intervals as absolute instants.
//
// The date components are passed unnormalized (d may be 0 or 32) and
// time.Date normalizes them, which is also what makes the minute arithmetic
// correct across a DST boundary: the offset is resolved by the location at that
// wall time rather than by adding a fixed duration.
func (s *Schedule) segmentsOn(y int, m time.Month, d int) [][2]time.Time {
	loc := s.Location()
	weekday := time.Date(y, m, d, 12, 0, 0, 0, loc).Weekday()
	ivs := s.days[weekday]
	if len(ivs) == 0 {
		return nil
	}
	out := make([][2]time.Time, 0, len(ivs))
	for _, iv := range ivs {
		start := time.Date(y, m, d, 0, iv.StartMin, 0, 0, loc)
		end := time.Date(y, m, d, 0, iv.EndMin, 0, 0, loc)
		if !end.After(start) {
			continue
		}
		out = append(out, [2]time.Time{start, end})
	}
	return out
}

// segmentsBetween collects every open segment that can touch [from, to),
// merged so overlapping ones are counted once.
//
// It starts a day early because an overnight shift that began before `from`
// belongs to the previous weekday's interval list.
func (s *Schedule) segmentsBetween(from, to time.Time) [][2]time.Time {
	loc := s.Location()
	start := from.In(loc)
	end := to.In(loc)

	y, m, d := start.Date()
	// Anchored at midnight, not midday: the loop bound is "this day can still
	// touch [from, to)", and a midday anchor would drop the final day whenever
	// `to` falls in its morning.
	day := time.Date(y, m, d-1, 0, 0, 0, 0, loc)

	var segs [][2]time.Time
	for !day.After(end) {
		dy, dm, dd := day.Date()
		segs = append(segs, s.segmentsOn(dy, dm, dd)...)
		day = day.AddDate(0, 0, 1)
	}
	return mergeSegments(segs)
}

// mergeSegments coalesces overlapping or touching ranges.
//
// Without it, an overnight shift running into the next day's own morning window
// would describe the same wall-clock minutes twice and Elapsed would hand back
// more business time than exists.
func mergeSegments(segs [][2]time.Time) [][2]time.Time {
	if len(segs) < 2 {
		return segs
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i][0].Before(segs[j][0]) })

	out := make([][2]time.Time, 0, len(segs))
	out = append(out, segs[0])
	for _, seg := range segs[1:] {
		last := &out[len(out)-1]
		if seg[0].After(last[1]) {
			out = append(out, seg)
			continue
		}
		if seg[1].After(last[1]) {
			last[1] = seg[1]
		}
	}
	return out
}

// overlap is the intersection length of two half-open ranges.
func overlap(aStart, aEnd, bStart, bEnd time.Time) time.Duration {
	start, end := aStart, aEnd
	if bStart.After(start) {
		start = bStart
	}
	if bEnd.Before(end) {
		end = bEnd
	}
	if !end.After(start) {
		return 0
	}
	return end.Sub(start)
}
