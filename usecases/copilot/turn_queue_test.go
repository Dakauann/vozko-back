package copilot_usecase

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"vozko/domain/copilot"
	"vozko/usecases/agentloop"
)

func TestAFollowUpRunsInTheSameTurnUntilNothingIsLeft(t *testing.T) {
	reg := NewTurnRegistry(testLimits())
	turn, _ := reg.Begin(context.Background(), "th1", "u1")
	_, live, leave := turn.Subscribe()
	defer leave()
	left := 2
	runs := 0
	next := func() TurnFunc {
		if left == 0 {
			return nil
		}
		left--
		return func(ctx context.Context, emit agentloop.Emit) error {
			runs++
			emit("done", map[string]int{"run": runs})
			return nil
		}
	}
	turn.start(func(ctx context.Context, emit agentloop.Emit) error {
		emit("done", map[string]int{"run": 0})
		return nil
	}, next)
	got := types(waitClosed(t, live))
	if len(got) != 3 || runs != 2 {
		t.Fatalf("events %v runs %d, want the first answer and two follow-ups", got, runs)
	}
	if reg.Running("th1") {
		t.Fatal("the turn must end once nothing is left")
	}
}

func TestAStoppedTurnStartsNoFollowUp(t *testing.T) {
	reg := NewTurnRegistry(testLimits())
	turn, _ := reg.Begin(context.Background(), "th1", "u1")
	_, live, leave := turn.Subscribe()
	defer leave()
	asked := false
	turn.start(func(ctx context.Context, emit agentloop.Emit) error {
		if err := reg.Stop("th1", "u1"); err != nil {
			t.Errorf("stop: %v", err)
		}
		return nil
	}, func() TurnFunc {
		asked = true
		return func(context.Context, agentloop.Emit) error { t.Error("a stopped turn ran a follow-up"); return nil }
	})
	waitClosed(t, live)
	if asked {
		t.Fatal("after stop the queue must not even be consulted")
	}
}

func TestAFailedAnswerStartsNoFollowUp(t *testing.T) {
	reg := NewTurnRegistry(testLimits())
	turn, _ := reg.Begin(context.Background(), "th1", "u1")
	_, live, leave := turn.Subscribe()
	defer leave()
	turn.start(func(ctx context.Context, emit agentloop.Emit) error {
		return errors.New("falhou")
	}, func() TurnFunc {
		return func(context.Context, agentloop.Emit) error { t.Error("a failed turn ran a follow-up"); return nil }
	})
	got := types(waitClosed(t, live))
	if len(got) != 1 || got[0] != "error" {
		t.Fatalf("events = %v", got)
	}
}

func TestHoldAddsToTheRunningTurn(t *testing.T) {
	reg := NewTurnRegistry(testLimits())
	turn, _ := reg.Begin(context.Background(), "th1", "u1")
	release := holdOpen(turn)
	defer close(release)
	var got *Turn
	err := reg.Hold("th1", "u1", func() (bool, error) {
		t.Error("a running turn must not ask whether the thread is waiting")
		return false, nil
	}, func(running *Turn) error {
		got = running
		return nil
	})
	if err != nil || got != turn {
		t.Fatalf("hold: %v, turn %v", err, got)
	}
	if err := reg.Hold("th1", "intruder", func() (bool, error) { return true, nil }, func(*Turn) error { return nil }); !errors.Is(err, copilot.ErrTurnForbidden) {
		t.Fatalf("another user held the turn: %v", err)
	}
}

func TestHoldWithoutATurnNeedsAWaitingThread(t *testing.T) {
	reg := NewTurnRegistry(testLimits())
	added := 0
	add := func(running *Turn) error {
		if running != nil {
			t.Error("there is no running turn to add to")
		}
		added++
		return nil
	}
	if err := reg.Hold("th1", "u1", func() (bool, error) { return false, nil }, add); !errors.Is(err, copilot.ErrTurnNotRunning) {
		t.Fatalf("an idle thread must refuse, got %v", err)
	}
	if err := reg.Hold("th1", "u1", func() (bool, error) { return true, nil }, add); err != nil {
		t.Fatalf("a thread waiting for approval must accept: %v", err)
	}
	broken := errors.New("db")
	if err := reg.Hold("th1", "u1", func() (bool, error) { return false, broken }, add); !errors.Is(err, broken) {
		t.Fatalf("a failed check must refuse, got %v", err)
	}
	if added != 1 {
		t.Fatalf("added %d, want only the waiting thread", added)
	}
}

func TestEveryHeldMessageIsEitherDrainedOrRefusedNeverLost(t *testing.T) {
	for round := 0; round < 30; round++ {
		reg := NewTurnRegistry(testLimits())
		turn, _ := reg.Begin(context.Background(), "th1", "u1")
		_, live, leave := turn.Subscribe()
		var queued, drained, refused int64
		var mu sync.Mutex
		next := func() TurnFunc {
			mu.Lock()
			defer mu.Unlock()
			if queued == 0 {
				return nil
			}
			return func(context.Context, agentloop.Emit) error {
				mu.Lock()
				defer mu.Unlock()
				drained += queued
				queued = 0
				return nil
			}
		}
		turn.start(func(context.Context, agentloop.Emit) error { return nil }, next)
		var wg sync.WaitGroup
		for range 20 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := reg.Hold("th1", "u1", func() (bool, error) { return false, nil }, func(*Turn) error {
					mu.Lock()
					queued++
					mu.Unlock()
					return nil
				})
				if errors.Is(err, copilot.ErrTurnNotRunning) {
					atomic.AddInt64(&refused, 1)
				}
			}()
		}
		wg.Wait()
		waitClosed(t, live)
		leave()
		mu.Lock()
		if queued != 0 || drained+refused != 20 {
			t.Fatalf("round %d: queued %d drained %d refused %d", round, queued, drained, refused)
		}
		mu.Unlock()
	}
}

func TestADeliveryStartsTheReplayAfterWhatIsAlreadySaved(t *testing.T) {
	reg := NewTurnRegistry(testLimits())
	turn, _ := reg.Begin(context.Background(), "th1", "u1")
	release := holdOpen(turn)
	defer close(release)
	turn.Emit(agentloop.EventAssistantDelta, map[string]string{"text": "parte um"})
	turn.Emit(EventDelivered, map[string]string{"clientId": "c1"})
	turn.Emit(agentloop.EventAssistantDelta, map[string]string{"text": "parte dois"})
	replay, _, leave := turn.Subscribe()
	defer leave()
	got := types(replay)
	if len(got) != 2 || got[0] != EventDelivered || got[1] != agentloop.EventAssistantDelta {
		t.Fatalf("replay = %v, want the delivery and what followed it", got)
	}
}
