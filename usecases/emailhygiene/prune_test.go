package emailhygiene

import (
	"context"
	"testing"
	"time"
)

type countingPrune struct {
	left    int64
	calls   int
	cutoffs []time.Time
}

func (c *countingPrune) Prune(_ context.Context, before time.Time, limit int) (int64, error) {
	c.calls++
	c.cutoffs = append(c.cutoffs, before)
	n := min(c.left, int64(limit))
	c.left -= n
	return n, nil
}

func TestPruneDeletesInBoundedBatchesUntilNothingIsLeft(t *testing.T) {
	domains, activity := &countingPrune{left: 12000}, &countingPrune{left: 3}
	lists := &fakeLists{}
	p := NewPruner(PrunerDeps{Domains: domains, Activity: activity, Lists: lists})
	p.now = func() time.Time { return clock }
	if err := p.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if domains.calls != 3 || activity.calls != 1 {
		t.Fatalf("domain batches=%d activity batches=%d", domains.calls, activity.calls)
	}
	if !domains.cutoffs[0].Equal(clock.AddDate(0, 0, -180)) || !activity.cutoffs[0].Equal(clock.Add(-730*24*time.Hour)) {
		t.Fatalf("cutoffs domain=%v activity=%v", domains.cutoffs[0], activity.cutoffs[0])
	}
	if lists.kept != KeepDisposableLists {
		t.Fatal("old disposable lists are pruned too")
	}
}

func TestPruneStopsAfterItsBatchBudget(t *testing.T) {
	domains := &countingPrune{left: 1 << 40}
	p := NewPruner(PrunerDeps{Domains: domains, Activity: &countingPrune{}, Lists: &fakeLists{}})
	if err := p.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if domains.calls != PruneMaxBatches {
		t.Fatalf("batches = %d, a run stops at %d", domains.calls, PruneMaxBatches)
	}
}
