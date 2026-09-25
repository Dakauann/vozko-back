package copilottools

import (
	"context"
	"testing"
	"time"

	"vozko/domain/calendar"
	"vozko/domain/copilot"
)

type fakeCreateEvent struct {
	inputs []calendar.CreateEventInput
	err    error
}

func (f *fakeCreateEvent) Execute(in calendar.CreateEventInput) (*calendar.CalendarEvent, error) {
	f.inputs = append(f.inputs, in)
	if f.err != nil {
		return nil, f.err
	}
	return &calendar.CalendarEvent{ID: "ev1", Title: in.Title, StartTime: in.StartTime}, nil
}

func eventArgs() map[string]interface{} {
	return map[string]interface{}{"title": "Ligar para Maria", "start": "2026-09-26T10:00:00-03:00", "end": "2026-09-26T10:30:00-03:00"}
}

func TestCreateCalendarEventNeedsApprovalAndCalendarCreate(t *testing.T) {
	if m := NewCreateCalendarEventTool(&fakeCreateEvent{}).Meta(); !m.Mutating || m.Resource != "calendar" || m.Action != "create" {
		t.Fatalf("meta = %+v", m)
	}
}

func TestCreateCalendarEventWritesToTheUsersOwnCalendar(t *testing.T) {
	create := &fakeCreateEvent{}
	res := NewCreateCalendarEventTool(create).Execute(context.Background(), member(), eventArgs())
	if res.Status != copilot.StatusOK || len(create.inputs) != 1 {
		t.Fatalf("status %s: %s", res.Status, res.Message)
	}
	in := create.inputs[0]
	if in.UserID != "u-1" || in.WorkspaceID != "ws-1" || in.StartTime.UTC().Format(time.RFC3339) != "2026-09-26T13:00:00Z" {
		t.Fatalf("input = %+v", in)
	}
}

func TestCreateCalendarEventRefusesAmbiguousTimes(t *testing.T) {
	create := &fakeCreateEvent{}
	args := eventArgs()
	args["start"] = "sexta às 10h"
	if res := NewCreateCalendarEventTool(create).Execute(context.Background(), member(), args); res.Status != copilot.StatusError || len(create.inputs) != 0 {
		t.Fatalf("status %s", res.Status)
	}
}

func TestCreateCalendarEventExplainsRefusals(t *testing.T) {
	for _, err := range []error{calendar.ErrGoogleNotConnected, calendar.ErrInvalidTimeRange} {
		res := NewCreateCalendarEventTool(&fakeCreateEvent{err: err}).Execute(context.Background(), member(), eventArgs())
		if res.Status != copilot.StatusError || res.Message == "falha ao criar o compromisso" {
			t.Fatalf("%v: %+v", err, res)
		}
	}
}
