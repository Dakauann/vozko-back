package campaignguard

import (
	"context"
	"testing"
	"time"

	"vozko/domain/campaign"
)

type steppedClock struct{ at time.Time }

func (c *steppedClock) now() time.Time { return c.at }

func TestTheWorkspaceReadsAreSharedAcrossAWholeCampaignRun(t *testing.T) {
	ticks := &steppedClock{at: guardNow}
	days := &fixedDays{days: 3}
	sends := &sendLog{last: map[string]time.Time{}}
	spam, err := NewSpamGuard(NewMemoSpamPolicy(days, WorkspaceMemoTTL, ticks.now), sends, newClaimBook(), clock)
	if err != nil {
		t.Fatal(err)
	}
	leads := &leadBook{facts: map[string]campaign.LeadFacts{"l-1": reachable()}}
	eligible, err := NewEligibility(leads, spam)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if _, err := eligible.Check(context.Background(), "ws-1", "l-1", "bp-1"); err != nil {
			t.Fatal(err)
		}
	}
	if days.calls != 1 {
		t.Fatalf("config reads = %d, want one for the run", days.calls)
	}
	if len(leads.batches) != 50 || len(sends.batches) != 50 {
		t.Fatalf("lead reads = %d send reads = %d, want one per entry so changes apply at once", len(leads.batches), len(sends.batches))
	}
	if _, err := eligible.Check(context.Background(), "ws-2", "l-1", "bp-1"); err != nil {
		t.Fatal(err)
	}
	if days.calls != 2 {
		t.Fatalf("another workspace must read its own policy: %d", days.calls)
	}
	ticks.at = ticks.at.Add(WorkspaceMemoTTL + time.Second)
	if _, err := eligible.Check(context.Background(), "ws-1", "l-1", "bp-1"); err != nil {
		t.Fatal(err)
	}
	if days.calls != 3 {
		t.Fatalf("an expired read must be refreshed: %d", days.calls)
	}
}

func TestTheMemoNeverKeepsAFailedRead(t *testing.T) {
	ticks := &steppedClock{at: guardNow}
	days := &fixedDays{err: errBoom}
	memo := NewMemoSpamPolicy(days, WorkspaceMemoTTL, ticks.now)
	if _, err := memo.SpamProtectionDays(context.Background(), "ws-1"); err == nil {
		t.Fatal("a failed read must surface")
	}
	days.err, days.days = nil, 4
	got, err := memo.SpamProtectionDays(context.Background(), "ws-1")
	if err != nil || got != 4 || days.calls != 2 {
		t.Fatalf("after a failure the next read goes through: (%d, %v) calls %d", got, err, days.calls)
	}
}

func TestTheMemoRefusesWithoutItsSource(t *testing.T) {
	if _, err := NewMemoSpamPolicy(nil, WorkspaceMemoTTL, nil).SpamProtectionDays(context.Background(), "ws-1"); err == nil {
		t.Fatal("a memo without a source must refuse")
	}
}
