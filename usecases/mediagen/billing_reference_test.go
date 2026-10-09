package mediagen_usecase

import (
	"context"
	"testing"

	"vozko/domain/mediagen"
)

func TestAGenerationStartedFromAThreadIsChargedToIt(t *testing.T) {
	f := newFixture(t)
	f.gen.cost = 53_000
	req := imageRequest()
	req.BillingReference = "aichat:th-1"
	job, err := f.svc.Request(context.Background(), req, "u-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Process(context.Background(), &mediagen.QueueMessage{JobID: job.ID}); err != nil {
		t.Fatal(err)
	}
	if len(f.bill.events) != 1 || f.bill.events[0].reference != "aichat:th-1" {
		t.Fatalf("the charge must name the thread, got %+v", f.bill.events)
	}
}

func TestALateCostIsStillChargedToTheThread(t *testing.T) {
	f := newFixture(t)
	f.audio.unpriced, f.audio.generation = true, "gen-1"
	req := musicRequest()
	req.BillingReference = "aichat:th-2"
	job, err := f.svc.Request(context.Background(), req, "u-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Process(context.Background(), &mediagen.QueueMessage{JobID: job.ID}); err != nil {
		t.Fatal(err)
	}
	f.late.costs["gen-1"] = 40_000
	if err := f.svc.Settle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.bill.events) != 1 || f.bill.events[0].reference != "aichat:th-2" {
		t.Fatalf("a settled charge must name the thread, got %+v", f.bill.events)
	}
}

func TestAGenerationFromElsewhereNamesNoThread(t *testing.T) {
	f := newFixture(t)
	f.gen.cost = 53_000
	f.requestAndProcess(t)
	if len(f.bill.events) != 1 || f.bill.events[0].reference != "" {
		t.Fatalf("a generation outside Elo carries no thread, got %+v", f.bill.events)
	}
}
