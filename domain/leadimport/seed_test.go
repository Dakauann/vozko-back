package leadimport

import (
	"fmt"
	"testing"

	"vozko/domain/unofficial_whatsapp"
)

func seedTargets(n int) []unofficial_whatsapp.SeedTarget {
	out := make([]unofficial_whatsapp.SeedTarget, n)
	for i := range out {
		out[i] = unofficial_whatsapp.SeedTarget{Number: fmt.Sprintf("55119%08d", 10000000+i), Name: fmt.Sprintf("Pessoa %d", i)}
	}
	return out
}

func TestSeedBatchesSplitTheTargetsTheWayTheQueueDoes(t *testing.T) {
	script := &unofficial_whatsapp.SeedScript{Bodies: []string{"Oi {{1}}"}, MaxMessages: 4}
	cases := []struct {
		name     string
		targets  int
		script   *unofficial_whatsapp.SeedScript
		batches  int
		scripted int
	}{
		{"no targets", 0, nil, 0, 0},
		{"plain conversations in batches of the queue size", unofficial_whatsapp.SeedBatchSize*2 + 1, nil, 3, 0},
		{"scripted ones first, in their own smaller batches", unofficial_whatsapp.MaxScriptedTargets + 10, script,
			unofficial_whatsapp.MaxScriptedTargets/unofficial_whatsapp.ScriptedSeedBatchSize + 1, unofficial_whatsapp.MaxScriptedTargets},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SeedBatches("ws-1", seedTargets(tc.targets), tc.script)
			scripted, total := 0, 0
			for _, b := range got {
				total += len(b.Targets)
				if b.Script != nil {
					scripted += len(b.Targets)
				}
				if b.WorkspaceID != "ws-1" {
					t.Fatalf("batch workspace = %q", b.WorkspaceID)
				}
			}
			if len(got) != tc.batches || scripted != tc.scripted || total != tc.targets {
				t.Fatalf("batches = %d, scripted = %d, total = %d", len(got), scripted, total)
			}
		})
	}
}

func TestSeedOutcomeRecordsEachBatchOnce(t *testing.T) {
	batches := SeedBatches("ws-1", seedTargets(unofficial_whatsapp.SeedBatchSize*3), nil)
	var out SeedOutcome
	if next := out.Resume(batches); next != 0 || out.Done(batches) {
		t.Fatalf("next = %d, outcome = %+v", next, out)
	}
	out.Begin()
	out.Sent(unofficial_whatsapp.SeedQueued{Targets: unofficial_whatsapp.SeedBatchSize})
	if out.Batches != 1 || out.Sending != 1 || out.Queued != unofficial_whatsapp.SeedBatchSize || out.Unconfirmed != 0 {
		t.Fatalf("outcome = %+v", out)
	}
	out.Begin()
	if next := out.Resume(batches); next != 2 || out.Unconfirmed != unofficial_whatsapp.SeedBatchSize || out.Batches != 2 {
		t.Fatalf("after a crash while the second batch was sent: next = %d, outcome = %+v", next, out)
	}
	if next := out.Resume(batches); next != 2 || out.Unconfirmed != unofficial_whatsapp.SeedBatchSize {
		t.Fatalf("a second resume counted the batch again: %+v", out)
	}
	out.Begin()
	out.Sent(unofficial_whatsapp.SeedQueued{Targets: unofficial_whatsapp.SeedBatchSize})
	if !out.Done(batches) || out.Queued != 2*unofficial_whatsapp.SeedBatchSize {
		t.Fatalf("outcome = %+v", out)
	}
}

func TestSeedOutcomeStopsAtAQueueFailure(t *testing.T) {
	batches := SeedBatches("ws-1", seedTargets(unofficial_whatsapp.SeedBatchSize*2), nil)
	var out SeedOutcome
	out.Resume(batches)
	out.Begin()
	out.Failed()
	if out.Error != SeedFailed || out.Sending != out.Batches || !out.Done(batches) || out.Queued != 0 {
		t.Fatalf("outcome = %+v", out)
	}
	if next := out.Resume(batches); next != 0 || out.Unconfirmed != 0 {
		t.Fatalf("a refused batch is not unconfirmed: %+v", out)
	}
}

func TestSeedOutcomeResumeNeverReachesPastTheBatches(t *testing.T) {
	batches := SeedBatches("ws-1", seedTargets(3), nil)
	out := SeedOutcome{Batches: 0, Sending: 4}
	if next := out.Resume(batches); next != len(batches) || out.Unconfirmed != 3 || !out.Done(batches) {
		t.Fatalf("next = %d, outcome = %+v", next, out)
	}
}
