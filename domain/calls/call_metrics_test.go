package calls

import (
	"sync/atomic"
	"testing"
)

type mockCallMetrics struct {
	dialing  atomic.Int64
	ongoing  atomic.Int64
	finished atomic.Int64
}

func (m *mockCallMetrics) IncCallDialing()  { m.dialing.Add(1) }
func (m *mockCallMetrics) DecCallDialing()  { m.dialing.Add(-1) }
func (m *mockCallMetrics) IncCallOngoing()  { m.ongoing.Add(1) }
func (m *mockCallMetrics) DecCallOngoing()  { m.ongoing.Add(-1) }
func (m *mockCallMetrics) IncCallFinished() { m.finished.Add(1) }

func TestCallStateMachine_DialFailPath(t *testing.T) {
	m := &mockCallMetrics{}
	sm := NewCallStateMachine(m)
	sm.Dialing()
	sm.LeaveDialing()
	sm.Finished()

	if m.dialing.Load() != 0 {
		t.Errorf("dialing gauge = %d, want 0 after dial fail", m.dialing.Load())
	}
	if m.ongoing.Load() != 0 {
		t.Errorf("ongoing gauge = %d, want 0 after dial fail", m.ongoing.Load())
	}
	if m.finished.Load() != 1 {
		t.Errorf("finished counter = %d, want 1", m.finished.Load())
	}
}

func TestCallStateMachine_HappyPath(t *testing.T) {
	m := &mockCallMetrics{}
	sm := NewCallStateMachine(m)
	sm.Dialing()
	sm.Answered()
	sm.LeaveDialing()
	sm.LeaveOngoing()
	sm.Finished()

	if m.dialing.Load() != 0 {
		t.Errorf("dialing gauge = %d, want 0 after happy path", m.dialing.Load())
	}
	if m.ongoing.Load() != 0 {
		t.Errorf("ongoing gauge = %d, want 0 after happy path", m.ongoing.Load())
	}
	if m.finished.Load() != 1 {
		t.Errorf("finished counter = %d, want 1", m.finished.Load())
	}
}

func TestCallStateMachine_DeferPattern(t *testing.T) {
	m := &mockCallMetrics{}

	call := func(dialSucceeds bool) {
		sm := NewCallStateMachine(m)
		sm.Dialing()
		defer sm.LeaveDialing()
		defer sm.Finished()
		if !dialSucceeds {
			return
		}
		sm.Answered()
		defer sm.LeaveOngoing()
	}

	call(false)

	if m.dialing.Load() != 0 {
		t.Errorf("after dial fail: dialing = %d, want 0", m.dialing.Load())
	}
	if m.ongoing.Load() != 0 {
		t.Errorf("after dial fail: ongoing = %d, want 0", m.ongoing.Load())
	}
	if m.finished.Load() != 1 {
		t.Errorf("after dial fail: finished = %d, want 1", m.finished.Load())
	}

	call(true)

	if m.dialing.Load() != 0 {
		t.Errorf("after happy path: dialing = %d, want 0", m.dialing.Load())
	}
	if m.ongoing.Load() != 0 {
		t.Errorf("after happy path: ongoing = %d, want 0", m.ongoing.Load())
	}
	if m.finished.Load() != 2 {
		t.Errorf("after happy path: finished = %d, want 2", m.finished.Load())
	}
}

func TestCallStateMachine_LeaveDialingIdempotentAfterAnswered(t *testing.T) {
	m := &mockCallMetrics{}
	sm := NewCallStateMachine(m)

	sm.Dialing()

	sm.Answered()

	sm.LeaveDialing()
	sm.LeaveOngoing()
	sm.Finished()

	if m.dialing.Load() != 0 {
		t.Errorf("dialing = %d, want 0 (no double-decrement)", m.dialing.Load())
	}
	if m.ongoing.Load() != 0 {
		t.Errorf("ongoing = %d, want 0", m.ongoing.Load())
	}
}

func TestCallStateMachine_FinishedIdempotent(t *testing.T) {
	m := &mockCallMetrics{}
	sm := NewCallStateMachine(m)

	sm.Dialing()
	sm.Answered()
	sm.Finished()
	sm.Finished()
	sm.Finished()
	sm.LeaveDialing()
	sm.LeaveOngoing()

	if m.finished.Load() != 1 {
		t.Errorf("finished = %d, want 1 (idempotent)", m.finished.Load())
	}
}

func TestCallStateMachine_NilMetricsIsNoOp(t *testing.T) {
	sm := NewCallStateMachine(nil)
	sm.Dialing()
	sm.Answered()
	sm.LeaveDialing()
	sm.LeaveOngoing()
	sm.Finished()
	sm.Finished()
}

func TestCallStateMachine_ConcurrentCalls(t *testing.T) {
	m := &mockCallMetrics{}
	const n = 100
	done := make(chan struct{}, n)

	for i := 0; i < n; i++ {
		go func() {
			sm := NewCallStateMachine(m)
			sm.Dialing()
			defer sm.LeaveDialing()
			defer sm.Finished()
			sm.Answered()
			defer sm.LeaveOngoing()
			done <- struct{}{}
		}()
	}

	for i := 0; i < n; i++ {
		<-done
	}

	if m.dialing.Load() != 0 {
		t.Errorf("dialing = %d, want 0 after %d concurrent calls", m.dialing.Load(), n)
	}
	if m.ongoing.Load() != 0 {
		t.Errorf("ongoing = %d, want 0 after %d concurrent calls", m.ongoing.Load(), n)
	}
	if m.finished.Load() != n {
		t.Errorf("finished = %d, want %d", m.finished.Load(), n)
	}
}
