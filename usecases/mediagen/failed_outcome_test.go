package mediagen_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/mediagen"
)

func refusedMusic(t *testing.T, f *fixture) string {
	t.Helper()
	f.audio.err = mediagen.Charged("gen-9", errors.New("the model refused political content"))
	return musicJob(t, f)
}

func TestAFailureStillAwaitingItsCostIsAlreadyFailedForTheUser(t *testing.T) {
	f := newFixture(t)
	id := refusedMusic(t, f)
	job := f.jobs.job(id)
	if job.Status != mediagen.StatusSettling || job.Outcome() != mediagen.StatusFailed {
		t.Fatalf("job %+v: the cost is still reconciled but the user must see the failure", job)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	finished, err := f.svc.Wait(ctx, "ws-1", id)
	if err != nil || finished.Outcome() != mediagen.StatusFailed {
		t.Fatalf("waited %v job %+v", err, finished)
	}
}

func TestAFailureKeepsTheProviderReason(t *testing.T) {
	f := newFixture(t)
	job := f.jobs.job(refusedMusic(t, f))
	if !strings.Contains(job.FailureDetail, "refused political content") {
		t.Fatalf("detail %q", job.FailureDetail)
	}
}

func TestARetryAfterAFailureAwaitingItsCostGeneratesAgain(t *testing.T) {
	f := newFixture(t)
	first := refusedMusic(t, f)
	f.audio.err = nil
	again, err := f.svc.Request(context.Background(), musicRequest(), "u-1")
	if err != nil || again.ID == first {
		t.Fatalf("again %+v err %v: a failed job must not be handed back as the active one", again, err)
	}
}

func TestAFailureThatNeverGetsACostKeepsItsReasonAndIsNotBilled(t *testing.T) {
	f := newFixture(t)
	id := refusedMusic(t, f)
	f.svc.now = func() time.Time { return clock.Add(mediagen.SettleWindow + time.Minute) }
	if err := f.svc.Settle(context.Background()); err != nil {
		t.Fatal(err)
	}
	job := f.jobs.job(id)
	if job.Status != mediagen.StatusFailed || job.FailureCode != mediagen.FailureGeneration || len(f.bill.events) != 0 {
		t.Fatalf("job %+v billed %+v", job, f.bill.events)
	}
}

func TestAFailureAwaitingItsCostIsBilledOnceTheCostArrives(t *testing.T) {
	f := newFixture(t)
	id := refusedMusic(t, f)
	f.late.costs["gen-9"] = 3_000
	if err := f.svc.Settle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if job := f.jobs.job(id); job.Status != mediagen.StatusFailed || len(f.bill.events) != 1 || f.bill.events[0].cost != 3_000 {
		t.Fatalf("job %+v billed %+v", job, f.bill.events)
	}
}
