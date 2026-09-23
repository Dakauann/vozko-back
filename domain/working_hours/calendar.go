package working_hours

import (
	"errors"
	"time"
)

const MaxCalendarScanDays = 3700

var ErrRangeTooWide = errors.New("working hours: range exceeds the supported calendar span")

type civilDate struct {
	Year  int
	Month time.Month
	Day   int
}

func civilOf(t time.Time) civilDate {
	y, m, d := t.Date()
	return civilDate{Year: y, Month: m, Day: d}
}

func (s *Schedule) isHoliday(y int, m time.Month, d int) bool {
	if s == nil || len(s.holidays) == 0 {
		return false
	}
	ref := time.Date(y, m, d, 12, 0, 0, 0, s.Location())
	_, found := s.holidays[civilOf(ref)]
	return found
}

func (s *Schedule) OpenDays(from, to time.Time) (int, bool) {
	if s == nil {
		return 0, false
	}
	if !to.After(from) {
		return 0, true
	}
	loc := s.Location()
	start := from.In(loc)
	end := to.In(loc)

	y, m, d := start.Date()
	day := time.Date(y, m, d-1, 0, 0, 0, 0, loc)

	count := 0
	for scanned := 0; !day.After(end); scanned++ {
		if scanned > MaxCalendarScanDays {
			return 0, false
		}
		dy, dm, dd := day.Date()
		for _, seg := range s.segmentsOn(dy, dm, dd) {
			if overlap(seg[0], seg[1], from, to) > 0 {
				count++
				break
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return count, true
}

func (s *Schedule) OpenMinutes(from, to time.Time) (int, bool) {
	if s == nil {
		return 0, false
	}
	if !to.After(from) {
		return 0, true
	}
	if spanDays(from, to) > MaxCalendarScanDays {
		return 0, false
	}
	return int(s.Elapsed(from, to).Minutes()), true
}

func (s *Schedule) Progress(from, to, at time.Time) (elapsed, total int, ok bool) {
	if s == nil || !to.After(from) {
		return 0, 0, false
	}
	total, ok = s.OpenMinutes(from, to)
	if !ok || total <= 0 {
		return 0, 0, false
	}
	cursor := at
	if cursor.Before(from) {
		cursor = from
	}
	if cursor.After(to) {
		cursor = to
	}
	elapsed, ok = s.OpenMinutes(from, cursor)
	if !ok {
		return 0, 0, false
	}
	if elapsed > total {
		elapsed = total
	}
	return elapsed, total, true
}

func spanDays(from, to time.Time) int {
	return int(to.Sub(from).Hours()/24) + 2
}
