package calendar

import (
	"errors"
	"testing"
	"time"
)

type listingCalendar struct {
	GoogleCalendarAPI
	events []*CalendarEvent
	err    error
}

func (l listingCalendar) ListGoogleEvents(string, time.Time, time.Time, string, int) ([]*CalendarEvent, error) {
	return l.events, l.err
}

func TestSlotConflictRefusesWhenTheCalendarCannotBeRead(t *testing.T) {
	start := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	for name, check := range map[string]ConflictCheck{
		"google listing fails": {Google: listingCalendar{err: errors.New("google down")}},
		"local events fail":    {Google: listingCalendar{}, Local: localEvents{err: errors.New("db down")}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := check.Conflict("ws", start, end); !errors.Is(err, ErrConflictCheckUnavailable) || ErrorCode(err) != CodeConflictCheckUnavailable {
				t.Fatalf("err = %v, code %q", err, ErrorCode(err))
			}
		})
	}
}

func TestSlotConflictFindsTheFirstOverlappingBusyEvent(t *testing.T) {
	start := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	events := []*CalendarEvent{
		{Title: "livre", StartTime: start, EndTime: end, Transparency: "transparent"},
		{Title: "antes", StartTime: start.Add(-time.Hour), EndTime: start},
		{Title: "consulta", StartTime: start.Add(30 * time.Minute), EndTime: end.Add(time.Hour)},
	}
	conflict, err := ConflictCheck{Google: listingCalendar{events: events}}.Conflict("ws", start, end)
	if err != nil || conflict == nil || conflict.Title != "consulta" {
		t.Fatalf("conflict = %+v, %v", conflict, err)
	}
	free, err := ConflictCheck{Google: listingCalendar{events: events[:2]}}.Conflict("ws", start, end)
	if err != nil || free != nil {
		t.Fatalf("free slot reported busy: %+v, %v", free, err)
	}
}

func TestErrorCodeNamesSlotConflicts(t *testing.T) {
	if ErrorCode(ErrSlotConflict) != CodeSlotConflict {
		t.Fatal(ErrorCode(ErrSlotConflict))
	}
}
