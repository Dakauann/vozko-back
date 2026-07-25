package calendar_usecase

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/calendar"
)

// stubUpdateUC captures the UpdateEventInput the reschedule use case delegates, so the
// tests can assert the computed times and the conflict flag without exercising the real
// provider/persistence path.
type stubUpdateUC struct {
	captured calendar.UpdateEventInput
	result   *calendar.CalendarEvent
	err      error
}

func (s *stubUpdateUC) Execute(in calendar.UpdateEventInput) (*calendar.CalendarEvent, error) {
	s.captured = in
	if s.err != nil {
		return nil, s.err
	}
	if s.result != nil {
		return s.result, nil
	}
	ev := &calendar.CalendarEvent{GoogleEventID: "g1", Title: "meeting"}
	if in.StartTime != nil {
		ev.StartTime = *in.StartTime
	}
	if in.EndTime != nil {
		ev.EndTime = *in.EndTime
	}
	return ev, nil
}

func TestReschedule_PreservesOriginalDuration(t *testing.T) {
	id := uuid.NewString()
	t0 := time.Date(2026, 4, 10, 14, 0, 0, 0, time.UTC)
	repo := &watchRepoMock{event: &calendar.CalendarEvent{ID: id, StartTime: t0, EndTime: t0.Add(90 * time.Minute)}}
	upd := &stubUpdateUC{}
	uc := NewRescheduleEventUseCase(repo, &watchGoogleMock{}, upd)

	newStart := time.Date(2026, 4, 11, 16, 0, 0, 0, time.UTC)
	if _, err := uc.Execute(calendar.RescheduleEventInput{EventID: id, WorkspaceID: "ws-1", NewStartTime: newStart}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if upd.captured.StartTime == nil || !upd.captured.StartTime.Equal(newStart) {
		t.Fatalf("expected new start %v, got %v", newStart, upd.captured.StartTime)
	}
	wantEnd := newStart.Add(90 * time.Minute)
	if upd.captured.EndTime == nil || !upd.captured.EndTime.Equal(wantEnd) {
		t.Fatalf("expected preserved 90min end %v, got %v", wantEnd, upd.captured.EndTime)
	}
	if !upd.captured.CheckConflict {
		t.Fatalf("expected CheckConflict=true by default")
	}
}

func TestReschedule_ExplicitEndWins(t *testing.T) {
	upd := &stubUpdateUC{}
	uc := NewRescheduleEventUseCase(&watchRepoMock{}, &watchGoogleMock{}, upd)

	start := time.Date(2026, 4, 11, 9, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	if _, err := uc.Execute(calendar.RescheduleEventInput{
		EventID: "gcal_g1", WorkspaceID: "ws-1", NewStartTime: start, NewEndTime: &end,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if upd.captured.EndTime == nil || !upd.captured.EndTime.Equal(end) {
		t.Fatalf("expected explicit end %v, got %v", end, upd.captured.EndTime)
	}
}

func TestReschedule_DurationComputesEnd(t *testing.T) {
	upd := &stubUpdateUC{}
	uc := NewRescheduleEventUseCase(&watchRepoMock{}, &watchGoogleMock{}, upd)

	start := time.Date(2026, 4, 11, 9, 0, 0, 0, time.UTC)
	if _, err := uc.Execute(calendar.RescheduleEventInput{
		EventID: "gcal_g1", WorkspaceID: "ws-1", NewStartTime: start, DurationMinutes: 45,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := start.Add(45 * time.Minute)
	if upd.captured.EndTime == nil || !upd.captured.EndTime.Equal(want) {
		t.Fatalf("expected 45min end %v, got %v", want, upd.captured.EndTime)
	}
}

func TestReschedule_SkipConflictCheckDisablesGuard(t *testing.T) {
	upd := &stubUpdateUC{}
	uc := NewRescheduleEventUseCase(&watchRepoMock{}, &watchGoogleMock{}, upd)

	start := time.Date(2026, 4, 11, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	if _, err := uc.Execute(calendar.RescheduleEventInput{
		EventID: "gcal_g1", WorkspaceID: "ws-1", NewStartTime: start, NewEndTime: &end, SkipConflictCheck: true,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if upd.captured.CheckConflict {
		t.Fatalf("expected CheckConflict=false when SkipConflictCheck is set")
	}
}

func TestReschedule_PropagatesSlotConflict(t *testing.T) {
	upd := &stubUpdateUC{err: calendar.ErrSlotConflict}
	uc := NewRescheduleEventUseCase(&watchRepoMock{}, &watchGoogleMock{}, upd)

	start := time.Date(2026, 4, 11, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	_, err := uc.Execute(calendar.RescheduleEventInput{EventID: "gcal_g1", WorkspaceID: "ws-1", NewStartTime: start, NewEndTime: &end})
	if !errors.Is(err, calendar.ErrSlotConflict) {
		t.Fatalf("expected ErrSlotConflict, got %v", err)
	}
}

func TestReschedule_ValidationErrors(t *testing.T) {
	uc := NewRescheduleEventUseCase(&watchRepoMock{}, &watchGoogleMock{}, &stubUpdateUC{})

	if _, err := uc.Execute(calendar.RescheduleEventInput{WorkspaceID: "ws-1", NewStartTime: time.Now()}); !errors.Is(err, calendar.ErrEventNotFound) {
		t.Fatalf("empty event id should be ErrEventNotFound, got %v", err)
	}
	if _, err := uc.Execute(calendar.RescheduleEventInput{EventID: "gcal_g1", WorkspaceID: "ws-1"}); !errors.Is(err, calendar.ErrInvalidTimeRange) {
		t.Fatalf("zero start should be ErrInvalidTimeRange, got %v", err)
	}
	start := time.Date(2026, 4, 11, 9, 0, 0, 0, time.UTC)
	badEnd := start.Add(-time.Hour)
	if _, err := uc.Execute(calendar.RescheduleEventInput{EventID: "gcal_g1", WorkspaceID: "ws-1", NewStartTime: start, NewEndTime: &badEnd}); !errors.Is(err, calendar.ErrInvalidTimeRange) {
		t.Fatalf("end before start should be ErrInvalidTimeRange, got %v", err)
	}
}

// The conflict guard lives in the update use case (the reschedule engine) and is
// exercised via CheckConflict; these verify a busy new slot is refused and a free one
// (with the event's own slot correctly excluded) proceeds.
func TestUpdateEvent_CheckConflictBlocksOccupiedSlot(t *testing.T) {
	id := uuid.NewString()
	start := time.Date(2026, 4, 27, 14, 0, 0, 0, time.UTC)
	existing := &calendar.CalendarEvent{ID: id, WorkspaceID: "ws-1", GoogleEventID: "self-g", Title: "Consulta", StartTime: start, EndTime: start.Add(time.Hour), Status: "confirmed"}
	self := &calendar.CalendarEvent{GoogleEventID: "self-g", StartTime: start, EndTime: start.Add(time.Hour)}
	other := &calendar.CalendarEvent{GoogleEventID: "other-g", Title: "Ocupado", StartTime: start.Add(30 * time.Minute), EndTime: start.Add(90 * time.Minute), Transparency: "opaque"}
	repo := &watchRepoMock{connection: validWatchConnection(), event: existing}
	google := &watchGoogleMock{listGoogleEventsResult: []*calendar.CalendarEvent{self, other}}
	uc := NewUpdateEventUseCase(repo, google)

	ns, ne := start, start.Add(time.Hour)
	_, err := uc.Execute(calendar.UpdateEventInput{EventID: id, WorkspaceID: "ws-1", StartTime: &ns, EndTime: &ne, CheckConflict: true})
	if !errors.Is(err, calendar.ErrSlotConflict) {
		t.Fatalf("expected ErrSlotConflict, got %v", err)
	}
	if google.updatedGoogleEventID != "" {
		t.Fatal("a conflicting reschedule must not update the Google event")
	}
}

func TestUpdateEvent_CheckConflictAllowsFreeSlotExcludingSelf(t *testing.T) {
	id := uuid.NewString()
	start := time.Date(2026, 4, 27, 14, 0, 0, 0, time.UTC)
	existing := &calendar.CalendarEvent{ID: id, WorkspaceID: "ws-1", GoogleEventID: "self-g", Title: "Consulta", StartTime: start, EndTime: start.Add(time.Hour), Status: "confirmed"}
	self := &calendar.CalendarEvent{GoogleEventID: "self-g", StartTime: start, EndTime: start.Add(time.Hour)}
	transparent := &calendar.CalendarEvent{GoogleEventID: "free-g", StartTime: start, EndTime: start.Add(time.Hour), Transparency: "transparent"}
	repo := &watchRepoMock{connection: validWatchConnection(), event: existing}
	google := &watchGoogleMock{listGoogleEventsResult: []*calendar.CalendarEvent{self, transparent}}
	uc := NewUpdateEventUseCase(repo, google)

	ns, ne := start, start.Add(time.Hour)
	if _, err := uc.Execute(calendar.UpdateEventInput{EventID: id, WorkspaceID: "ws-1", StartTime: &ns, EndTime: &ne, CheckConflict: true}); err != nil {
		t.Fatalf("expected free slot to reschedule, got %v", err)
	}
	if google.updatedGoogleEventID != "self-g" {
		t.Fatalf("expected UpdateGoogleEvent on self-g, got %q", google.updatedGoogleEventID)
	}
}

func TestReschedule_ConstructorNilGuards(t *testing.T) {
	if NewRescheduleEventUseCase(nil, &watchGoogleMock{}, &stubUpdateUC{}) != nil {
		t.Fatal("nil repo should yield nil use case")
	}
	if NewRescheduleEventUseCase(&watchRepoMock{}, nil, &stubUpdateUC{}) != nil {
		t.Fatal("nil google should yield nil use case")
	}
	if NewRescheduleEventUseCase(&watchRepoMock{}, &watchGoogleMock{}, nil) != nil {
		t.Fatal("nil update use case should yield nil use case")
	}
}
