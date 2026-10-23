package calendar

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"vozko/domain/shared"
)

type Slot struct {
	Start time.Time
	End   time.Time
}

type WorkingHours struct {
	Start    Clock
	End      Clock
	Days     map[time.Weekday]bool
	Location *time.Location
}

const localEventsPageSize = 500

func ParseWorkDays(raw string) map[time.Weekday]bool {
	days := map[time.Weekday]bool{time.Monday: true, time.Tuesday: true, time.Wednesday: true, time.Thursday: true, time.Friday: true}
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "all_week":
		days[time.Saturday], days[time.Sunday] = true, true
	case "weekdays_saturday":
		days[time.Saturday] = true
	}
	return days
}

func WithLocalEvents(repo Repository, workspaceID string, from, to time.Time, googleEvents []*CalendarEvent) ([]*CalendarEvent, error) {
	if repo == nil {
		return googleEvents, nil
	}
	local, err := repo.ListEvents(ListEventsInput{WorkspaceID: workspaceID, From: &from, To: &to, Pagination: shared.Pagination{Page: 1, PageSize: localEventsPageSize}})
	if err != nil {
		return nil, fmt.Errorf("falha ao consultar eventos locais: %w", err)
	}
	if local == nil {
		return googleEvents, nil
	}
	seen := make(map[string]bool, len(googleEvents))
	for _, ev := range googleEvents {
		if ev != nil && ev.GoogleEventID != "" {
			seen[ev.GoogleEventID] = true
		}
	}
	merged := append([]*CalendarEvent(nil), googleEvents...)
	for _, ev := range local.Items {
		if ev != nil && (ev.GoogleEventID == "" || !seen[ev.GoogleEventID]) {
			merged = append(merged, ev)
		}
	}
	return merged, nil
}

func FreeSlots(from, to time.Time, events []*CalendarEvent, slotDuration time.Duration, hours WorkingHours) []Slot {
	busy := mergedBusy(from, to, events)
	loc := hours.Location
	var slots []Slot
	for current := from; current.Before(to); {
		day := current.In(loc)
		next := time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc)
		if hours.Days == nil || hours.Days[day.Weekday()] {
			windowStart, windowEnd := laterOf(hours.Start.On(day, loc), from), earlierOf(hours.End.On(day, loc), to)
			if windowEnd.After(windowStart) {
				slots = append(slots, slotsInWindow(windowStart, windowEnd, busy, slotDuration)...)
			}
		}
		current = next
	}
	return slots
}

func mergedBusy(from, to time.Time, events []*CalendarEvent) []Slot {
	busy := make([]Slot, 0, len(events))
	for _, ev := range events {
		if ev != nil && ev.Transparency != "transparent" && ev.EndTime.After(from) && ev.StartTime.Before(to) {
			busy = append(busy, Slot{Start: ev.StartTime, End: ev.EndTime})
		}
	}
	sort.Slice(busy, func(i, j int) bool { return busy[i].Start.Before(busy[j].Start) })
	merged := make([]Slot, 0, len(busy))
	for _, b := range busy {
		last := len(merged) - 1
		if last >= 0 && !b.Start.After(merged[last].End) {
			merged[last].End = laterOf(merged[last].End, b.End)
			continue
		}
		merged = append(merged, b)
	}
	return merged
}

func slotsInWindow(windowStart, windowEnd time.Time, busy []Slot, duration time.Duration) []Slot {
	var slots []Slot
	cursor := windowStart
	for _, b := range busy {
		if !b.End.After(cursor) {
			continue
		}
		if !b.Start.Before(windowEnd) {
			break
		}
		slots = append(slots, consecutiveSlots(cursor, earlierOf(b.Start, windowEnd), duration)...)
		cursor = b.End
	}
	if cursor.Before(windowEnd) {
		slots = append(slots, consecutiveSlots(cursor, windowEnd, duration)...)
	}
	return slots
}

func consecutiveSlots(from, to time.Time, duration time.Duration) []Slot {
	var slots []Slot
	if duration <= 0 {
		return slots
	}
	for cursor := from; !cursor.Add(duration).After(to); cursor = cursor.Add(duration) {
		slots = append(slots, Slot{Start: cursor, End: cursor.Add(duration)})
	}
	return slots
}

func laterOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earlierOf(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
