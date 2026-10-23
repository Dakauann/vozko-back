package calendar

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/shared"
)

func TestFreeSlotsSkipBusyTimeClosedDaysAndHoursOutsideWork(t *testing.T) {
	loc := saoPaulo(t)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, loc) }
	events := []*CalendarEvent{
		{StartTime: at(23, 9, 0), EndTime: at(23, 10, 0)},
		{StartTime: at(23, 9, 30), EndTime: at(23, 10, 30)},
		{StartTime: at(23, 11, 0), EndTime: at(23, 12, 0), Transparency: "transparent"},
	}
	hours := WorkingHours{Start: Clock{Hour: 8}, End: Clock{Hour: 12}, Days: ParseWorkDays("weekdays"), Location: loc}
	slots := FreeSlots(at(23, 0, 0), at(25, 0, 0), events, time.Hour, hours)
	var got []string
	for _, slot := range slots {
		got = append(got, slot.Start.Format("02 15:04")+"-"+slot.End.Format("15:04"))
	}
	want := []string{"23 08:00-09:00", "23 10:30-11:30"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("slots = %v, want %v", got, want)
	}
}

func TestParseWorkDaysKnowsTheThreeSchedules(t *testing.T) {
	for raw, saturday := range map[string]bool{"weekdays": false, "weekdays_saturday": true, "all_week": true, "": false} {
		days := ParseWorkDays(raw)
		if days[time.Saturday] != saturday || !days[time.Monday] || days[time.Sunday] != (raw == "all_week") {
			t.Fatalf("%q = %v", raw, days)
		}
	}
}

type localEvents struct {
	Repository
	items []*CalendarEvent
	err   error
}

func (l localEvents) ListEvents(ListEventsInput) (*shared.PaginatedResult[*CalendarEvent], error) {
	return &shared.PaginatedResult[*CalendarEvent]{Items: l.items}, l.err
}

func TestWithLocalEventsAddsUnsyncedEventsAndFailsWhenTheyCannotBeRead(t *testing.T) {
	now := time.Now()
	google := []*CalendarEvent{{GoogleEventID: "g1"}}
	merged, err := WithLocalEvents(localEvents{items: []*CalendarEvent{{GoogleEventID: "g1"}, {ID: "local"}}}, "ws", now, now, google)
	if err != nil || len(merged) != 2 || merged[1].ID != "local" {
		t.Fatalf("merged = %+v, %v", merged, err)
	}
	if _, err := WithLocalEvents(localEvents{err: errors.New("down")}, "ws", now, now, google); err == nil {
		t.Fatal("an unreadable local calendar was treated as free")
	}
}
