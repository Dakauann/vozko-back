package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type flightState struct {
	*ownedState
	values map[string]string
	getErr error
}

func newFlightState() *flightState {
	return &flightState{ownedState: newOwnedState(), values: map[string]string{}}
}

func (s *flightState) SetString(key, value string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
	return nil
}

func (s *flightState) GetString(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return "", s.getErr
	}
	if held, ok := s.held[key]; ok {
		return held, nil
	}
	return s.values[key], nil
}

func testFlight(state *flightState) SharedFlight {
	return SharedFlight{State: state, Owner: state, TTL: time.Minute, Poll: time.Millisecond, Keep: time.Minute}
}

func TestSharedFlightRunsConcurrentIdenticalCallsOnce(t *testing.T) {
	flight := testFlight(newFlightState())
	var runs atomic.Int32
	release := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]string, 10)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := flight.Share(context.Background(), "count:a", func(context.Context) ([]byte, error) {
				runs.Add(1)
				<-release
				return []byte("42"), nil
			})
			if err != nil {
				t.Error(err)
			}
			results[i] = string(value)
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if runs.Load() != 1 {
		t.Fatalf("runs = %d, want one shared run", runs.Load())
	}
	for _, r := range results {
		if r != "42" {
			t.Fatalf("results = %v", results)
		}
	}
}

func TestSharedFlightNeverServesAFinishedRunToALaterCall(t *testing.T) {
	flight := testFlight(newFlightState())
	runs := 0
	for i := 0; i < 2; i++ {
		if _, err := flight.Share(context.Background(), "count:a", func(context.Context) ([]byte, error) {
			runs++
			return []byte("x"), nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if runs != 2 {
		t.Fatalf("runs = %d, a call after a finished run counts again", runs)
	}
}

func TestSharedFlightLetsAWaiterRunWhenTheLeaderFails(t *testing.T) {
	flight := testFlight(newFlightState())
	started := make(chan struct{})
	fail := make(chan struct{})
	errLeader := errors.New("leader failed")
	go func() {
		_, _ = flight.Share(context.Background(), "count:a", func(context.Context) ([]byte, error) {
			close(started)
			<-fail
			return nil, errLeader
		})
	}()
	<-started
	done := make(chan []byte)
	go func() {
		value, err := flight.Share(context.Background(), "count:a", func(context.Context) ([]byte, error) { return []byte("mine"), nil })
		if err != nil {
			t.Error(err)
		}
		done <- value
	}()
	time.Sleep(20 * time.Millisecond)
	close(fail)
	if got := string(<-done); got != "mine" {
		t.Fatalf("the waiter got %q, want its own run", got)
	}
}

func TestSharedFlightKeepsDifferentKeysApart(t *testing.T) {
	flight := testFlight(newFlightState())
	for _, key := range []string{"count:a", "count:b"} {
		value, err := flight.Share(context.Background(), key, func(context.Context) ([]byte, error) { return []byte(key), nil })
		if err != nil || string(value) != key {
			t.Fatalf("%s = %q, %v", key, value, err)
		}
	}
}

func TestSharedFlightFailsClosed(t *testing.T) {
	ran := false
	run := func(context.Context) ([]byte, error) {
		ran = true
		return nil, nil
	}
	if _, err := (SharedFlight{}).Share(context.Background(), "k", run); !errors.Is(err, ErrSharedRunUnavailable) {
		t.Fatalf("unwired = %v", err)
	}
	broken := newFlightState()
	broken.err = errors.New("redis down")
	if _, err := testFlight(broken).Share(context.Background(), "k", run); !errors.Is(err, ErrSharedRunUnavailable) {
		t.Fatalf("a failing store = %v", err)
	}
	busy := newFlightState()
	busy.held["k:run"] = "someone"
	busy.getErr = errors.New("redis down")
	if _, err := testFlight(busy).Share(context.Background(), "k", run); !errors.Is(err, ErrSharedRunUnavailable) {
		t.Fatalf("a failing read while waiting = %v", err)
	}
	if ran {
		t.Fatal("the run went ahead without the coordination store")
	}
	if _, err := NewSharedFlight(nil, time.Minute, time.Millisecond, time.Minute); !errors.Is(err, ErrSharedRunUnavailable) {
		t.Fatalf("NewSharedFlight(nil) = %v", err)
	}
}

func TestSharedFlightStopsWaitingWhenTheCallerLeaves(t *testing.T) {
	state := newFlightState()
	state.held["k:run"] = "someone"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := testFlight(state).Share(ctx, "k", func(context.Context) ([]byte, error) { return nil, nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("an abandoned wait = %v", err)
	}
}
