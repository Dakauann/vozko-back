package rcs

import "testing"

func TestEnumsKnowTheirValues(t *testing.T) {
	cases := []struct {
		name  string
		valid func() bool
		want  bool
	}{
		{"class basic", ClassBasic.Valid, true},
		{"class single", ClassSingle.Valid, true},
		{"class empty", BillingClass("").Valid, false},
		{"class unknown", BillingClass("premium").Valid, false},
		{"sender draft", SenderDraft.Valid, true},
		{"sender submitted", SenderSubmitted.Valid, true},
		{"sender awaiting", SenderAwaitingConfirmation.Valid, true},
		{"sender launched", SenderLaunched.Valid, true},
		{"sender paused", SenderPaused.Valid, true},
		{"sender rejected", SenderRejected.Valid, true},
		{"sender empty", SenderStatus("").Valid, false},
		{"sender unknown", SenderStatus("live").Valid, false},
		{"paused by platform", PausedByPlatform.Valid, true},
		{"paused by carrier", PausedByCarrier.Valid, true},
		{"paused by unknown", PausedBy("someone").Valid, false},
		{"pause manual", PauseManual.Valid, true},
		{"pause complaints", PauseComplaints.Valid, true},
		{"pause opt out rate", PauseOptOutRate.Valid, true},
		{"pause unpaid", PauseUnpaidBalance.Valid, true},
		{"pause carrier", PauseCarrier.Valid, true},
		{"pause unknown", PauseReason("mood").Valid, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.valid(); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
