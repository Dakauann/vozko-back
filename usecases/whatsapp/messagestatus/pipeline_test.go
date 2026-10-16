package messagestatus_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/whatsapp/messagestatus"
)

type fakeLedger struct {
	calls        []string
	giveUpBefore time.Time
	recountAfter time.Time
	purgeCutoff  time.Time
	attributeErr error
}

func (f *fakeLedger) AttributePending(_ context.Context, giveUpBefore time.Time) (int64, error) {
	f.calls = append(f.calls, "attribute")
	f.giveUpBefore = giveUpBefore
	return 0, f.attributeErr
}

func (f *fakeLedger) RollUpPending(_ context.Context, recountAfter time.Time) (int64, error) {
	f.calls = append(f.calls, "rollup")
	f.recountAfter = recountAfter
	return 0, nil
}

func (f *fakeLedger) PurgeRolledUpBefore(_ context.Context, cutoff time.Time) (int64, error) {
	f.calls = append(f.calls, "purge")
	f.purgeCutoff = cutoff
	return 0, nil
}

var fixedNow = time.Date(2026, 10, 15, 20, 0, 0, 0, time.UTC)

func pipelineWith(ledger *fakeLedger) *Pipeline {
	p := NewPipeline(ledger)
	p.now = func() time.Time { return fixedNow }
	return p
}

func TestSweepAttributesBeforeRollingUpWithTheDomainWindows(t *testing.T) {
	ledger := &fakeLedger{}

	if err := pipelineWith(ledger).Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if len(ledger.calls) != 2 || ledger.calls[0] != "attribute" || ledger.calls[1] != "rollup" {
		t.Fatalf("calls %v", ledger.calls)
	}
	if !ledger.giveUpBefore.Equal(fixedNow.Add(-messagestatus.AttributionWait)) {
		t.Fatalf("give up before %v", ledger.giveUpBefore)
	}
	if !ledger.recountAfter.Equal(fixedNow.Add(-messagestatus.RecountHorizon)) {
		t.Fatalf("recount after %v", ledger.recountAfter)
	}
}

func TestSweepDoesNotRollUpWhenAttributionFails(t *testing.T) {
	boom := errors.New("db down")
	ledger := &fakeLedger{attributeErr: boom}

	if err := pipelineWith(ledger).Sweep(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("want the attribution error, got %v", err)
	}
	if len(ledger.calls) != 1 {
		t.Fatalf("roll up must wait for a successful attribution, calls %v", ledger.calls)
	}
}

func TestPurgeKeepsTheRetentionWindow(t *testing.T) {
	ledger := &fakeLedger{}

	if err := pipelineWith(ledger).Purge(context.Background()); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if !ledger.purgeCutoff.Equal(fixedNow.Add(-messagestatus.RawRetention)) {
		t.Fatalf("purge cutoff %v", ledger.purgeCutoff)
	}
	if messagestatus.RecountHorizon >= messagestatus.RawRetention {
		t.Fatal("hours are never recounted from raw events that may already be purged")
	}
}
