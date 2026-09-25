package cache

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recordingMemo struct {
	stored map[string][]byte
	calls  int
}

func (m *recordingMemo) Remember(
	ctx context.Context,
	key string,
	_ time.Duration,
	compute func(context.Context) ([]byte, error),
) ([]byte, error) {
	m.calls++
	if value, ok := m.stored[key]; ok {
		return value, nil
	}
	value, err := compute(ctx)
	if err != nil {
		return nil, err
	}
	m.stored[key] = value
	return value, nil
}

type payload struct {
	Count int    `json:"count"`
	Label string `json:"label"`
}

func TestRememberComputesOnceAndDecodesTheCachedCopy(t *testing.T) {
	memo := &recordingMemo{stored: map[string][]byte{}}
	computed := 0
	compute := func(context.Context) (*payload, error) {
		computed++
		return &payload{Count: 7, Label: "sete"}, nil
	}

	first, err := Remember(context.Background(), memo, "k", time.Minute, compute)
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}
	second, err := Remember(context.Background(), memo, "k", time.Minute, compute)
	if err != nil {
		t.Fatalf("Remember() second error = %v", err)
	}

	if computed != 1 {
		t.Fatalf("compute ran %d times, want 1", computed)
	}
	if *first != *second || second.Count != 7 || second.Label != "sete" {
		t.Fatalf("Remember() = %+v then %+v, want the same payload twice", first, second)
	}
}

func TestRememberWithoutAMemoAlwaysComputes(t *testing.T) {
	computed := 0
	compute := func(context.Context) (int, error) {
		computed++
		return computed, nil
	}
	for range 2 {
		if _, err := Remember[int](context.Background(), nil, "k", time.Minute, compute); err != nil {
			t.Fatalf("Remember() error = %v", err)
		}
	}
	if computed != 2 {
		t.Fatalf("compute ran %d times without a memo, want 2", computed)
	}
}

func TestRememberDoesNotCacheAFailure(t *testing.T) {
	memo := &recordingMemo{stored: map[string][]byte{}}
	boom := errors.New("boom")
	_, err := Remember(context.Background(), memo, "k", time.Minute, func(context.Context) (int, error) {
		return 0, boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Remember() error = %v, want %v", err, boom)
	}
	if _, cached := memo.stored["k"]; cached {
		t.Fatalf("Remember() cached the result of a failed computation")
	}
}

func TestRememberRejectsACorruptCachedCopy(t *testing.T) {
	memo := &recordingMemo{stored: map[string][]byte{"k": []byte("{not json")}}
	_, err := Remember(context.Background(), memo, "k", time.Minute, func(context.Context) (int, error) {
		return 1, nil
	})
	if err == nil {
		t.Fatalf("Remember() decoded a corrupt cached copy without an error")
	}
}
