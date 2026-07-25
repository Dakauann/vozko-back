package conversation

import "testing"

func TestCloseSourceValid(t *testing.T) {
	cases := []struct {
		s    CloseSource
		want bool
	}{
		{CloseSourceHuman, true},
		{CloseSourceAI, true},
		{CloseSourceSystem, true},
		{CloseSource("abandoned"), false},
		{CloseSource(""), false},
	}
	for _, tc := range cases {
		if got := tc.s.Valid(); got != tc.want {
			t.Fatalf("CloseSource(%q).Valid()=%v want %v", tc.s, got, tc.want)
		}
	}
}

func TestCloseReasonValid(t *testing.T) {
	if !CloseReasonManual.Valid() || !CloseReasonCustomerIdle.Valid() || !CloseReasonAIResolved.Valid() || !CloseReasonMaxAge.Valid() || !CloseReasonWorkflow.Valid() {
		t.Fatal("expected catalog reasons valid")
	}
	if CloseReason("abandoned").Valid() {
		t.Fatal("abandoned must not be a close reason")
	}
}

func TestClampAutoCloseIdleHours(t *testing.T) {
	if got := ClampAutoCloseIdleHours(0); got != DefaultAutoCloseIdleAfterHours {
		t.Fatalf("0 → %d want %d", got, DefaultAutoCloseIdleAfterHours)
	}
	if got := ClampAutoCloseIdleHours(-3); got != DefaultAutoCloseIdleAfterHours {
		t.Fatalf("neg → %d", got)
	}
	if got := ClampAutoCloseIdleHours(48); got != 48 {
		t.Fatalf("48 → %d", got)
	}
	if got := ClampAutoCloseIdleHours(999); got != MaxAutoCloseIdleAfterHours {
		t.Fatalf("999 → %d want %d", got, MaxAutoCloseIdleAfterHours)
	}
}

func TestConversationStatusStillThreeOnly(t *testing.T) {
	for _, s := range []ConversationStatus{"new", "ongoing", "finished"} {
		if !s.Valid() {
			t.Fatalf("%s should be valid", s)
		}
	}
	if ConversationStatus("abandoned").Valid() {
		t.Fatal("abandoned must not be a peer conversation status")
	}
}
