package livedecisions_usecase

import (
	"context"
	"testing"
	"time"

	ld "vozko/domain/livedecision"
)

func TestTheSummaryWindowIsBounded(t *testing.T) {
	h := newHarness()
	var since []int
	h.service.deps.Log = summarizing{onSince: func(days int) { since = append(since, days) }, now: t0.Add(time.Hour)}
	for _, days := range []int{0, 7, 400} {
		if _, err := h.service.Summarize(context.Background(), days); err != nil {
			t.Fatal(err)
		}
	}
	if since[0] != 1 || since[1] != 7 || since[2] != MaxSummaryDays {
		t.Fatalf("windows = %v", since)
	}
}

type summarizing struct {
	onSince func(days int)
	now     time.Time
}

func (s summarizing) Append(context.Context, ld.Record) error { return nil }
func (s summarizing) Summarize(_ context.Context, since time.Time) ([]ld.Summary, error) {
	s.onSince(int(s.now.Sub(since).Hours() / 24))
	return nil, nil
}
