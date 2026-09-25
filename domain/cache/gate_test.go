package cache

import (
	"context"
	"errors"
	"testing"
)

type stubGate struct {
	err      error
	released int
}

func (g *stubGate) Acquire(context.Context) (func(), error) {
	if g.err != nil {
		return nil, g.err
	}
	return func() { g.released++ }, nil
}

func TestGatedRunsInsideASlotAndFreesItAfterwards(t *testing.T) {
	gate := &stubGate{}
	ran := false

	err := Gated(context.Background(), gate, func(context.Context) error {
		ran = true
		if gate.released != 0 {
			t.Fatal("slot released before the work finished")
		}
		return nil
	})

	if err != nil || !ran || gate.released != 1 {
		t.Fatalf("err = %v, ran = %v, released = %d; want nil, true, 1", err, ran, gate.released)
	}
}

func TestGatedNeverRunsWithoutASlot(t *testing.T) {
	ran := false

	err := Gated(context.Background(), &stubGate{err: ErrGateBusy}, func(context.Context) error {
		ran = true
		return nil
	})

	if !errors.Is(err, ErrGateBusy) || ran {
		t.Fatalf("err = %v, ran = %v; want ErrGateBusy and no work", err, ran)
	}
}

func TestGatedWithoutAGateRunsDirectly(t *testing.T) {
	want := errors.New("compute failed")

	if err := Gated(context.Background(), nil, func(context.Context) error { return want }); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}
