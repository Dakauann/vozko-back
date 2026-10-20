package campaignlive

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
)

type countingCounter struct {
	counts *campaign.Counts
	err    error
	calls  int
}

func (c *countingCounter) CountByStatus(string) (*campaign.Counts, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	copied := *c.counts
	return &copied, nil
}

func progressFixture(t *testing.T) (*countingCounter, *memoryMemo, *time.Time, campaign.ProgressUseCase) {
	t.Helper()
	counter := &countingCounter{counts: &campaign.Counts{Total: 1000, Pending: 900, Sent: 100}}
	memo := newMemoryMemo()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	uc, err := NewProgress(ProgressDeps{Namespace: "official", Counter: counter, Memo: memo, Samples: newMemoryState(), Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewProgress: %v", err)
	}
	return counter, memo, &now, uc
}

func TestEveryViewerInsideTheFreshnessWindowSharesOneCount(t *testing.T) {
	counter, memo, _, uc := progressFixture(t)
	for i := 0; i < 25; i++ {
		if _, err := uc.Execute(context.Background(), running("camp-1")); err != nil {
			t.Fatalf("Execute = %v", err)
		}
	}
	if counter.calls != 1 {
		t.Fatalf("counted %d times for 25 viewers, want once", counter.calls)
	}
	if memo.ttls["official:progress:camp-1:RUNNING"] != campaign.ProgressFreshness(campaign.StatusRunning) {
		t.Fatalf("ttls = %v", memo.ttls)
	}
}

func TestTheStatusAlwaysComesFromTheCampaignJustRead(t *testing.T) {
	_, _, _, uc := progressFixture(t)
	if _, err := uc.Execute(context.Background(), running("camp-1")); err != nil {
		t.Fatalf("Execute = %v", err)
	}
	paused := running("camp-1")
	paused.Status = campaign.StatusPaused
	got, err := uc.Execute(context.Background(), paused)
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	if got.Status != campaign.StatusPaused || got.RefreshAfterSeconds != 0 {
		t.Fatalf("a paused campaign answered %+v", got)
	}
}

func TestThePaceIsMeasuredBetweenCountsAMinuteApart(t *testing.T) {
	counter, memo, now, uc := progressFixture(t)
	if _, err := uc.Execute(context.Background(), running("camp-1")); err != nil {
		t.Fatalf("Execute = %v", err)
	}
	memo.expire()
	*now = now.Add(time.Minute)
	counter.counts = &campaign.Counts{Total: 1000, Pending: 780, Sent: 220}

	got, err := uc.Execute(context.Background(), running("camp-1"))
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	if got.Pace == nil || got.Pace.ProcessedPerMinute != 120 || got.Pace.RemainingSeconds == nil || *got.Pace.RemainingSeconds != 390 {
		t.Fatalf("pace = %+v", got.Pace)
	}
}

func TestAFailedCountIsNotCachedAsProgress(t *testing.T) {
	counter, _, _, uc := progressFixture(t)
	counter.err = errors.New("db down")
	if _, err := uc.Execute(context.Background(), running("camp-1")); err == nil {
		t.Fatal("a failed count must surface")
	}
	counter.err = nil
	if _, err := uc.Execute(context.Background(), running("camp-1")); err != nil {
		t.Fatalf("the next read must count again: %v", err)
	}
}

func TestProgressNeedsItsLookups(t *testing.T) {
	if _, err := NewProgress(ProgressDeps{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("NewProgress = %v, want ErrUnavailable", err)
	}
	_, _, _, uc := progressFixture(t)
	if _, err := uc.Execute(context.Background(), campaign.LiveCampaign{}); !errors.Is(err, ErrCampaignMissing) {
		t.Fatalf("Execute without a campaign = %v", err)
	}
}
