package mediagen_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/mediagen"
)

func musicJob(t *testing.T, f *fixture) string {
	t.Helper()
	job, err := f.svc.Request(context.Background(), musicRequest(), "u-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Process(context.Background(), &mediagen.QueueMessage{JobID: job.ID}); err != nil {
		t.Fatal(err)
	}
	return job.ID
}

func TestACostMissingFromTheResponseIsLookedUpBeforeDelivering(t *testing.T) {
	f := newFixture(t)
	f.audio.unpriced, f.audio.generation = true, "gen-1"
	f.costs.costs["gen-1"] = 40_000
	job := f.jobs.job(musicJob(t, f))
	if job.Status != mediagen.StatusDone || len(f.bill.events) != 1 || f.bill.events[0].cost != 40_000 {
		t.Fatalf("job %+v billed %+v", job, f.bill.events)
	}
}

func TestACostStillMissingKeepsTheResultUntilItIsBilled(t *testing.T) {
	f := newFixture(t)
	f.audio.unpriced, f.audio.generation = true, "gen-1"
	id := musicJob(t, f)
	waiting := f.jobs.job(id)
	if waiting.Status != mediagen.StatusSettling || waiting.GenerationID != "gen-1" || waiting.MediaID == "" || len(f.bill.events) != 0 {
		t.Fatalf("job %+v billed %+v", waiting, f.bill.events)
	}
	if waiting.Status.Terminal() {
		t.Fatal("a result must not be handed out before it is billed")
	}
	if err := f.svc.Settle(context.Background()); err != nil || f.jobs.job(id).Status != mediagen.StatusSettling {
		t.Fatalf("settled without a cost: %v", err)
	}
	f.late.costs["gen-1"] = 40_000
	if err := f.svc.Settle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if done := f.jobs.job(id); done.Status != mediagen.StatusDone || len(f.bill.events) != 1 || f.bill.events[0].cost != 40_000 {
		t.Fatalf("job %+v billed %+v", done, f.bill.events)
	}
	if err := f.svc.Settle(context.Background()); err != nil || len(f.bill.events) != 1 {
		t.Fatalf("billed twice %+v", f.bill.events)
	}
}

func TestNoCostAndNoGenerationIDStoresAndBillsNothing(t *testing.T) {
	f := newFixture(t)
	f.audio.unpriced = true
	job := f.jobs.job(musicJob(t, f))
	if job.Status != mediagen.StatusFailed || job.FailureCode != mediagen.FailureCostUnreported || len(f.up.names) != 0 || len(f.bill.events) != 0 {
		t.Fatalf("job %+v stored %v billed %+v", job, f.up.names, f.bill.events)
	}
}

func TestAGenerationThatFailsAfterStartingIsStillBilled(t *testing.T) {
	f := newFixture(t)
	f.audio.err = mediagen.Charged("gen-9", errors.New("stream cut"))
	f.costs.costs["gen-9"] = 3_000
	job := f.jobs.job(musicJob(t, f))
	if job.Status != mediagen.StatusFailed || len(f.bill.events) != 1 || f.bill.events[0].cost != 3_000 || len(f.up.names) != 0 {
		t.Fatalf("job %+v billed %+v", job, f.bill.events)
	}
}

func TestAFailedGenerationWithoutACostYetIsBilledLaterAndStaysFailed(t *testing.T) {
	f := newFixture(t)
	f.audio.err = mediagen.Charged("gen-9", errors.New("stream cut"))
	id := musicJob(t, f)
	if job := f.jobs.job(id); job.Status != mediagen.StatusSettling || job.MediaID != "" {
		t.Fatalf("job %+v", job)
	}
	f.late.costs["gen-9"] = 3_000
	if err := f.svc.Settle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if job := f.jobs.job(id); job.Status != mediagen.StatusFailed || job.FailureCode != mediagen.FailureGeneration || len(f.bill.events) != 1 {
		t.Fatalf("job %+v billed %+v", job, f.bill.events)
	}
}

func TestAProviderErrorBeforeStartingIsNeverBilled(t *testing.T) {
	f := newFixture(t)
	f.audio.err = errors.New("status 400")
	job := f.jobs.job(musicJob(t, f))
	if job.Status != mediagen.StatusFailed || len(f.bill.events) != 0 || len(f.costs.asked) != 0 {
		t.Fatalf("job %+v billed %+v asked %v", job, f.bill.events, f.costs.asked)
	}
}

func TestARenderIsNeverPricedNorLookedUp(t *testing.T) {
	f := newFixture(t)
	f.video.unpriced = true
	job := run(t, f, videoRequest())
	if job.Status != mediagen.StatusDone || len(f.costs.asked) != 0 || len(f.bill.events) != 0 {
		t.Fatalf("job %+v asked %v", job, f.costs.asked)
	}
}

func TestASettlingJobCountsAsTheActiveOneForADoubleClick(t *testing.T) {
	f := newFixture(t)
	f.audio.unpriced, f.audio.generation = true, "gen-1"
	first := musicJob(t, f)
	again, err := f.svc.Request(context.Background(), musicRequest(), "u-1")
	if err != nil || again.ID != first || f.audio.calls != 1 {
		t.Fatalf("again %+v err %v calls %d", again, err, f.audio.calls)
	}
}

func TestSettlingGivesUpOnlyAfterTheWindow(t *testing.T) {
	f := newFixture(t)
	f.audio.unpriced, f.audio.generation = true, "gen-1"
	id := musicJob(t, f)
	f.svc.now = func() time.Time { return clock.Add(mediagen.SettleWindow - time.Minute) }
	if err := f.svc.Settle(context.Background()); err != nil || f.jobs.job(id).Status != mediagen.StatusSettling {
		t.Fatalf("expired early: %v", err)
	}
	f.svc.now = func() time.Time { return clock.Add(mediagen.SettleWindow + time.Minute) }
	if err := f.svc.Settle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if job := f.jobs.job(id); job.Status != mediagen.StatusFailed || job.FailureCode != mediagen.FailureCostUnreported {
		t.Fatalf("job %+v", job)
	}
}
