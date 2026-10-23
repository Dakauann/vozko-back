package emailcampaign_usecase

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"vozko/domain/emailcampaign"
	"vozko/domain/emailsending"
	"vozko/usecases/campaignqueue"
)

func (w *world) newAudience(n int) {
	w.t.Helper()
	people := make([]person, n)
	for i := range people {
		people[i] = person{email: fmt.Sprintf("p%04d@loja.com.br", i)}
	}
	w.hygiene.neverMailed = true
	w.audience(people...)
}

func (w *world) sendTestBatch(t *testing.T, c emailcampaign.Campaign) {
	t.Helper()
	for _, id := range w.pub.entryIDs() {
		w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: id})
	}
}

func (w *world) at(d time.Duration) {
	w.svc.deps.Now = func() time.Time { return now.Add(d) }
}

func TestANewListSendsOnlyItsTestBatchFirst(t *testing.T) {
	w := newWorld(t)
	w.newAudience(1000)
	c := w.draft(nil)
	r, err := w.svc.Review(context.Background(), w.actor(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Audience.NeverMailed != 1000 || r.TestBatch != 200 {
		t.Fatalf("review never mailed = %d, test batch = %d", r.Audience.NeverMailed, r.TestBatch)
	}
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got := len(w.pub.entryIDs()); got != 200 {
		t.Fatalf("enqueued %d, only the test batch goes out first", got)
	}
	got := w.campaign(c.ID)
	if got.TestBatch == nil || got.TestBatch.State != emailcampaign.TestBatchTesting || got.TestBatch.Size != 200 || got.TestBatch.LastID == "" {
		t.Fatalf("test batch = %+v", got.TestBatch)
	}
}

func TestAKnownListIsNotGated(t *testing.T) {
	w := newWorld(t)
	people := make([]person, 1000)
	for i := range people {
		people[i] = person{email: fmt.Sprintf("p%04d@loja.com.br", i)}
	}
	w.audience(people...)
	c := w.draft(nil)
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got := len(w.pub.entryIDs()); got != 1000 || w.campaign(c.ID).TestBatch != nil {
		t.Fatalf("enqueued %d, a list mailed before is sent whole", got)
	}
}

func TestACleanTestBatchReleasesTheRestAfterTheWait(t *testing.T) {
	w := newWorld(t)
	w.newAudience(1000)
	c := w.draft(nil)
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil); err != nil {
		t.Fatal(err)
	}
	w.sendTestBatch(t, c)
	if err := w.svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tb := w.campaign(c.ID).TestBatch; tb.DrainedAt == nil || tb.State != emailcampaign.TestBatchTesting {
		t.Fatalf("test batch = %+v, the drain is stamped and the decision waits", tb)
	}
	w.outcomes.set(c.ID, emailsending.RunOutcome{Delivered: 196, HardBounces: 4})
	w.at(5 * time.Minute)
	if err := w.svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.pub.entryIDs()) != 200 {
		t.Fatal("nothing more goes out before the ten minute wait")
	}
	w.at(11 * time.Minute)
	if err := w.svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := w.campaign(c.ID)
	if got.TestBatch.State != emailcampaign.TestBatchPassed || got.TestBatch.DecidedAt == nil || got.Status != emailcampaign.StatusRunning {
		t.Fatalf("campaign = %+v / %+v", got, got.TestBatch)
	}
	if len(w.pub.entryIDs()) != 1000 {
		t.Fatalf("enqueued %d, a passed test batch releases the rest", len(w.pub.entryIDs()))
	}
}

func TestATestBatchWithTooManyBouncesHoldsTheCampaignForGood(t *testing.T) {
	w := newWorld(t)
	w.newAudience(1000)
	c := w.draft(nil)
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil); err != nil {
		t.Fatal(err)
	}
	w.sendTestBatch(t, c)
	if err := w.svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.outcomes.set(c.ID, emailsending.RunOutcome{Delivered: 150, HardBounces: 7})
	w.at(time.Minute)
	if err := w.svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := w.campaign(c.ID)
	if got.Status != emailcampaign.StatusHeld || got.HoldReason != emailcampaign.HoldTestBatchBounces || got.TestBatch.State != emailcampaign.TestBatchFailed {
		t.Fatalf("campaign = %+v", got)
	}
	if len(w.pub.entryIDs()) != 200 {
		t.Fatal("a failed test batch never releases the rest")
	}
	note := w.notes.drafts[len(w.notes.drafts)-1]
	if note.EpisodeKey != testBatchEpisode(c.ID) {
		t.Fatalf("notification = %+v, a failed test batch is an episode", note)
	}
	if _, err := w.svc.Resume(context.Background(), w.actor(), c.ID); !errors.Is(err, emailcampaign.ErrTestBatchFailed) {
		t.Fatalf("resume err = %v", err)
	}
	if _, err := w.svc.Pause(context.Background(), w.actor(), c.ID); !errors.Is(err, emailcampaign.ErrTestBatchFailed) {
		t.Fatalf("pause err = %v, a pause and resume must not get around the gate", err)
	}
	if _, err := w.svc.Stop(context.Background(), w.actor(), c.ID); err != nil {
		t.Fatalf("stop err = %v", err)
	}
	if w.campaign(c.ID).Status != emailcampaign.StatusStopped || !slices.Contains(w.notes.resolved, testBatchEpisode(c.ID)) {
		t.Fatal("a stop ends the campaign and closes the episode")
	}
}

func TestATestBatchWithComplaintsFails(t *testing.T) {
	w := newWorld(t)
	w.newAudience(1000)
	c := w.draft(nil)
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil); err != nil {
		t.Fatal(err)
	}
	w.sendTestBatch(t, c)
	_ = w.svc.Tick(context.Background())
	w.outcomes.set(c.ID, emailsending.RunOutcome{Delivered: 200, Complaints: 1})
	w.at(time.Minute)
	if err := w.svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := w.campaign(c.ID); got.HoldReason != emailcampaign.HoldTestBatchComplaints {
		t.Fatalf("campaign = %+v, one complaint in two hundred is above a tenth of a percent", got)
	}
}

func TestAHeldTestBatchSendsNothingMore(t *testing.T) {
	w := newWorld(t)
	w.newAudience(1000)
	c := w.draft(nil)
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil); err != nil {
		t.Fatal(err)
	}
	ids := w.pub.entryIDs()
	w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: ids[0]})
	_, _ = w.campaigns.SwapStatus(context.Background(), c.ID, emailcampaign.StatusRunning, emailcampaign.StatusHeld, emailcampaign.HoldTestBatchBounces)
	before := len(w.gateway.calls)
	if res := w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: ids[1]}); res.Outcome != campaignqueue.OutcomeDrop {
		t.Fatalf("result = %+v", res)
	}
	if len(w.gateway.calls) != before {
		t.Fatal("a failed test batch never sends a retried recipient")
	}
}

func TestTheABWinnerWaitsForTheTestBatch(t *testing.T) {
	w := newWorld(t)
	w.newAudience(1000)
	c := w.draft(&emailcampaign.ABTest{VariantBSubject: "Outra oferta", TestPercent: 20, WinnerMetric: emailcampaign.WinnerOpens, DecideAfterMinutes: 30})
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil); err != nil {
		t.Fatal(err)
	}
	w.at(2 * time.Hour)
	if err := w.svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := w.campaign(c.ID); got.ABTest.Winner != "" {
		t.Fatalf("winner %q chosen while the test batch is still out", got.ABTest.Winner)
	}
}
