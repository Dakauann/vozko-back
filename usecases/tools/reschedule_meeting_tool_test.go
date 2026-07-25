package tools_usecase

import (
	"context"
	"strings"
	"testing"
	"time"

	"vozko/domain/calendar"
)

type stubRescheduleUC struct {
	got    calendar.RescheduleEventInput
	result *calendar.CalendarEvent
	err    error
}

func (s *stubRescheduleUC) Execute(in calendar.RescheduleEventInput) (*calendar.CalendarEvent, error) {
	s.got = in
	return s.result, s.err
}

func rescheduleConfig() map[string]interface{} {
	return map[string]interface{}{
		"__workspace_id": "ws-1",
		"timezone":       "America/Sao_Paulo",
		"send_updates":   "all",
	}
}

func TestRescheduleTool_ConstructorNilGuard(t *testing.T) {
	if NewRescheduleMeetingToolUseCase(nil) != nil {
		t.Fatal("nil use case should yield a nil handler")
	}
}

func TestRescheduleTool_MissingWorkspace(t *testing.T) {
	tool := NewRescheduleMeetingToolUseCase(&stubRescheduleUC{})
	res, _ := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{}, map[string]interface{}{
		"event_id": "g1", "start_time": "2026-04-11T15:00:00-03:00",
	})
	if !res.IsError {
		t.Fatalf("expected error result without workspace, got %+v", res)
	}
}

func TestRescheduleTool_MissingEventID(t *testing.T) {
	tool := NewRescheduleMeetingToolUseCase(&stubRescheduleUC{})
	res, _ := tool.ExecuteWithConfig(context.Background(), rescheduleConfig(), map[string]interface{}{
		"start_time": "2026-04-11T15:00:00-03:00",
	})
	if !res.IsError {
		t.Fatalf("expected error result without event_id, got %+v", res)
	}
}

func TestRescheduleTool_Success(t *testing.T) {
	start := time.Date(2026, 4, 11, 15, 0, 0, 0, time.UTC)
	stub := &stubRescheduleUC{result: &calendar.CalendarEvent{
		GoogleEventID: "g1",
		Title:         "Consulta",
		StartTime:     start,
		EndTime:       start.Add(30 * time.Minute),
		MeetingLink:   "https://meet.example/abc",
	}}
	tool := NewRescheduleMeetingToolUseCase(stub)

	res, err := tool.ExecuteWithConfig(context.Background(), rescheduleConfig(), map[string]interface{}{
		"event_id":   "g1",
		"start_time": "2026-04-11T15:00:00-03:00",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %+v", res)
	}
	if stub.got.EventID != "g1" || stub.got.WorkspaceID != "ws-1" {
		t.Fatalf("input not forwarded correctly: %+v", stub.got)
	}
	if stub.got.NewStartTime.IsZero() {
		t.Fatal("expected a parsed new start time")
	}
	out, _ := res.Result.(string)
	if !strings.Contains(out, "google_event_id") || !strings.Contains(out, "g1") {
		t.Fatalf("expected result JSON with google_event_id, got %q", out)
	}
}

func TestRescheduleTool_ErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"conflict", calendar.ErrSlotConflict, "ocupado"},
		{"not found", calendar.ErrEventNotFound, "não encontrei"},
		{"not connected", calendar.ErrGoogleNotConnected, "Google Calendar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool := NewRescheduleMeetingToolUseCase(&stubRescheduleUC{err: tc.err})
			res, _ := tool.ExecuteWithConfig(context.Background(), rescheduleConfig(), map[string]interface{}{
				"event_id": "g1", "start_time": "2026-04-11T15:00:00-03:00",
			})
			if !res.IsError {
				t.Fatalf("expected error result for %s", tc.name)
			}
			msg, _ := res.Result.(string)
			if !strings.Contains(strings.ToLower(msg), strings.ToLower(tc.want)) {
				t.Fatalf("expected message to mention %q, got %q", tc.want, msg)
			}
		})
	}
}
