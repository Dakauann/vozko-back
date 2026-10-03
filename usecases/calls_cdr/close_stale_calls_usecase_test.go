package calls_cdr_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/calls/cdr"
	"vozko/domain/shared"
)

type staleBook struct {
	cdr.Repository
	calls     map[string]*cdr.Call
	completed []cdr.CompleteInput
	cutoff    time.Time
}

func (s *staleBook) ListStale(startedBefore time.Time, limit int) ([]*cdr.Call, error) {
	s.cutoff = startedBefore
	var out []*cdr.Call
	for _, call := range s.calls {
		if !call.IsTerminal() && call.StartedAt.Before(startedBefore) && len(out) < limit {
			out = append(out, call)
		}
	}
	return out, nil
}

func (s *staleBook) GetByCallID(callID string) (*cdr.Call, error) {
	if call, ok := s.calls[callID]; ok {
		return call, nil
	}
	return nil, cdr.ErrCallNotFound
}

func (s *staleBook) Complete(input cdr.CompleteInput) error {
	s.completed = append(s.completed, input)
	s.calls[input.CallID].Status = input.Status
	return nil
}

func (s *staleBook) List(cdr.ListFilters) (*shared.PaginatedResult[*cdr.Call], error) {
	return nil, errors.New("unused")
}

func TestCallsLeftOpenByACrashAreClosedAsInterrupted(t *testing.T) {
	now := time.Date(2026, 9, 30, 20, 0, 0, 0, time.UTC)
	answeredAt := now.Add(-6 * time.Hour)
	book := &staleBook{calls: map[string]*cdr.Call{
		"ringing-forever": {CallID: "ringing-forever", Status: cdr.StatusInProgress, StartedAt: now.Add(-7 * time.Hour)},
		"talked-then-crashed": {CallID: "talked-then-crashed", Status: cdr.StatusInProgress, StartedAt: now.Add(-6*time.Hour - time.Minute), AnsweredAt: &answeredAt},
		"still-talking": {CallID: "still-talking", Status: cdr.StatusInProgress, StartedAt: now.Add(-time.Hour)},
	}}
	sweep := NewCloseStaleCallsUseCase(book, 5*time.Hour, func() time.Time { return now })

	closed, err := sweep.Execute()
	if err != nil || closed != 2 {
		t.Fatalf("Execute() = %d, %v, want 2 closed", closed, err)
	}
	if !book.cutoff.Equal(now.Add(-5 * time.Hour)) {
		t.Fatalf("cutoff = %v", book.cutoff)
	}
	byCall := map[string]cdr.CompleteInput{}
	for _, input := range book.completed {
		byCall[input.CallID] = input
	}
	ringing := byCall["ringing-forever"]
	if ringing.Status != cdr.StatusAbandoned || *ringing.EndReason != cdr.EndReasonInterrupted || !ringing.EndedAt.Equal(now.Add(-7*time.Hour)) {
		t.Fatalf("ringing call closed as %+v", ringing)
	}
	talked := byCall["talked-then-crashed"]
	if talked.Status != cdr.StatusCompleted || !talked.EndedAt.Equal(answeredAt) {
		t.Fatalf("answered call closed as %+v: its end is the last moment we know of", talked)
	}
	if _, touched := byCall["still-talking"]; touched {
		t.Fatal("a call within the maximum duration was closed")
	}
}
