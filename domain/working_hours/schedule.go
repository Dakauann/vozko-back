package working_hours

import (
	"errors"
	"sort"
	"time"
)

const MinutesPerDay = 24 * 60

const nextOpenHorizonDays = 14

var (
	ErrNoLocation         = errors.New("working hours: schedule has no timezone")
	ErrIntervalOutOfRange = errors.New("working hours: interval outside the supported range")
	ErrIntervalEmpty      = errors.New("working hours: interval ends at or before it starts")
	ErrIntervalOverlap    = errors.New("working hours: overlapping intervals on the same day")
	ErrNoOpenTime         = errors.New("working hours: schedule never opens")
)

type Interval struct {
	StartMin int
	EndMin   int
}

type Schedule struct {
	loc  *time.Location
	days [7][]Interval
}

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

func (s *Schedule) Location() *time.Location {
	if s == nil || s.loc == nil {
		return time.UTC
	}
	return s.loc
}

func (s *Schedule) IsOpen(at time.Time) bool {
	if s == nil {
		return true
	}
	local := at.In(s.Location())
	y, m, d := local.Date()
	for _, offset := range [2]int{-1, 0} {
		for _, seg := range s.segmentsOn(y, m, d+offset) {
			if !local.Before(seg[0]) && local.Before(seg[1]) {
				return true
			}
		}
	}
	return false
}

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

func Resolve(workspace, department *Schedule) *Schedule {
	if department != nil {
		return department
	}
	return workspace
}

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

func (s *Schedule) segmentsBetween(from, to time.Time) [][2]time.Time {
	loc := s.Location()
	start := from.In(loc)
	end := to.In(loc)

	y, m, d := start.Date()
	day := time.Date(y, m, d-1, 0, 0, 0, 0, loc)

	var segs [][2]time.Time
	for !day.After(end) {
		dy, dm, dd := day.Date()
		segs = append(segs, s.segmentsOn(dy, dm, dd)...)
		day = day.AddDate(0, 0, 1)
	}
	return mergeSegments(segs)
}

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
