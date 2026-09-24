package inbox_assignment

import (
	"slices"
	"testing"
)

func TestRouletteHandouts(t *testing.T) {
	// A roulette hand-out is an assignment the rescue sweep may take back when
	// the person never opens it. An AI handing off through the roulette is the
	// same situation as an inbound one; a workflow naming a member is a choice.
	cases := map[string]bool{
		TriggerInboundRR:                 true,
		TriggerAutomationHandoffRoulette: true,
		TriggerAutomationHandoff:         false,
		TriggerManual:                    false,
		TriggerRescue:                    false,
		TriggerAutomationGoverned:        false,
	}
	for trigger, want := range cases {
		if got := IsRouletteHandout(trigger); got != want {
			t.Errorf("IsRouletteHandout(%q) = %v, want %v", trigger, got, want)
		}
		if want && !slices.Contains(RescueCandidateTriggers, trigger) {
			t.Errorf("%q is a hand-out but not a rescue candidate", trigger)
		}
	}
	if !slices.Contains(RescueCandidateTriggers, TriggerRescue) {
		t.Error("a rescued assignment must stay a candidate for the next hop")
	}
}
