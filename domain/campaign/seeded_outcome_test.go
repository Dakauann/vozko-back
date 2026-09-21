package campaign

import (
	"errors"
	"testing"
)

func tally(statuses []SendStatus) map[SendStatus]int {
	out := map[SendStatus]int{}
	for _, s := range statuses {
		out[s]++
	}
	return out
}

func TestSeededOutcomeStatusesSplitsTheList(t *testing.T) {
	mix := &SeededOutcome{SentPercent: 30, FailedPercent: 10}

	got := mix.Statuses(100)
	if len(got) != 100 {
		t.Fatalf("Statuses returned %d entries, want 100", len(got))
	}

	counts := tally(got)
	if counts[SendStatusSent] != 30 {
		t.Fatalf("sent = %d, want 30", counts[SendStatusSent])
	}
	if counts[SendStatusFailed] != 10 {
		t.Fatalf("failed = %d, want 10", counts[SendStatusFailed])
	}
	if counts[SendStatusPending] != 60 {
		t.Fatalf("pending = %d, want 60", counts[SendStatusPending])
	}
}

func TestSeededOutcomeFeedsMetrics(t *testing.T) {
	counts := &Counts{Total: 100}
	for _, status := range (&SeededOutcome{SentPercent: 30, FailedPercent: 10}).Statuses(100) {
		switch status {
		case SendStatusSent:
			counts.Sent++
		case SendStatusFailed:
			counts.Failed++
		default:
			counts.Pending++
		}
	}

	metrics := NewMetrics(counts)
	if metrics.Dispatches != 30 {
		t.Fatalf("dispatches = %d, want 30", metrics.Dispatches)
	}
	if metrics.Processed != 40 {
		t.Fatalf("processed = %d, want 40", metrics.Processed)
	}
	if metrics.SuccessRate != 75 {
		t.Fatalf("success rate = %v, want 75", metrics.SuccessRate)
	}
}

func TestSeededOutcomeSpreadsRatherThanBunches(t *testing.T) {
	got := (&SeededOutcome{SentPercent: 50}).Statuses(100)
	if got[0] == got[1] && got[1] == got[2] && got[2] == got[3] &&
		got[4] == got[0] && got[5] == got[0] {
		t.Fatalf("first six entries are all %q: the mix is bunched, not spread", got[0])
	}

	again := (&SeededOutcome{SentPercent: 50}).Statuses(100)
	for i := range got {
		if got[i] != again[i] {
			t.Fatalf("entry %d differs between runs: %q then %q", i, got[i], again[i])
		}
	}
}

func TestSeededOutcomeSettlesNothingByDefault(t *testing.T) {
	var absent *SeededOutcome
	if statuses := absent.Statuses(10); statuses != nil {
		t.Fatalf("nil mix returned %v, want nil so the caller keeps its own default", statuses)
	}
	if statuses := (&SeededOutcome{}).Statuses(10); statuses != nil {
		t.Fatalf("zeroed mix returned %v, want nil", statuses)
	}
	if err := absent.Validate(); err != nil {
		t.Fatalf("nil mix is the ordinary campaign, got %v", err)
	}
}

func TestSeededOutcomeRefusesMoreThanTheWholeList(t *testing.T) {
	mix := &SeededOutcome{SentPercent: 70, FailedPercent: 40}
	if err := mix.Validate(); !errors.Is(err, ErrSeededOutcomeOverflow) {
		t.Fatalf("Validate = %v, want ErrSeededOutcomeOverflow", err)
	}

	clamped := &SeededOutcome{SentPercent: 140, FailedPercent: -5}
	clamped.Normalize()
	if clamped.SentPercent != 100 || clamped.FailedPercent != 0 {
		t.Fatalf("Normalize = %+v, want {100 0}", clamped)
	}
}

func TestSeededOutcomeLeavesNothingPendingWhenSharesFillTheList(t *testing.T) {
	for total := 1; total <= 200; total++ {
		for _, split := range [][2]int{{40, 60}, {50, 50}, {1, 99}, {100, 0}, {0, 100}, {33, 67}} {
			mix := &SeededOutcome{SentPercent: split[0], FailedPercent: split[1]}
			counts := tally(mix.Statuses(total))

			if counts[SendStatusPending] != 0 {
				t.Fatalf("%d targets at %d/%d left %d pending",
					total, split[0], split[1], counts[SendStatusPending])
			}
			if counts[SendStatusSent]+counts[SendStatusFailed] != total {
				t.Fatalf("%d targets at %d/%d settled %d",
					total, split[0], split[1],
					counts[SendStatusSent]+counts[SendStatusFailed])
			}
		}
	}
}

func TestSeededOutcomeSplitsThreeTargetsFortySixty(t *testing.T) {
	counts := tally((&SeededOutcome{SentPercent: 40, FailedPercent: 60}).Statuses(3))

	if counts[SendStatusSent] != 1 || counts[SendStatusFailed] != 2 {
		t.Fatalf("split = %d sent / %d failed, want 1 / 2",
			counts[SendStatusSent], counts[SendStatusFailed])
	}
}

func TestSeededOutcomeNeverOverfillsTheList(t *testing.T) {
	for total := 1; total <= 60; total++ {
		for sent := 0; sent <= 100; sent += 7 {
			for failed := 0; failed <= 100; failed += 11 {
				mix := &SeededOutcome{SentPercent: sent, FailedPercent: failed}
				statuses := mix.Statuses(total)
				if statuses == nil {
					continue
				}
				if len(statuses) != total {
					t.Fatalf("%d targets produced %d statuses", total, len(statuses))
				}
				counts := tally(statuses)
				if counts[SendStatusSent]+counts[SendStatusFailed]+counts[SendStatusPending] != total {
					t.Fatalf("%d targets at %d/%d do not add up: %+v",
						total, sent, failed, counts)
				}
				if counts[SendStatusFailed] < 0 {
					t.Fatalf("%d targets at %d/%d produced a negative bucket", total, sent, failed)
				}
			}
		}
	}
}
