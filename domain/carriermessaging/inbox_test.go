package carriermessaging

import (
	"testing"
	"time"
)

func TestBilledConversationLastsTwentyFourHours(t *testing.T) {
	opened := t0
	c := Conversation{ConversationOpenedAt: &opened}
	if !c.BilledConversationOpen(t0.Add(23*time.Hour + 59*time.Minute)) {
		t.Fatal("still open before 24h")
	}
	if c.BilledConversationOpen(t0.Add(24 * time.Hour)) {
		t.Fatal("closed at 24h")
	}
	if (Conversation{}).BilledConversationOpen(t0) {
		t.Fatal("never opened")
	}
}

func TestContactNameFallsBackToThePhone(t *testing.T) {
	if (Contact{Phone: "5511999990000"}).Name() != "5511999990000" {
		t.Fatal("phone fallback")
	}
	if (Contact{Phone: "5511999990000", DisplayName: " Ana "}).Name() != "Ana" {
		t.Fatal("display name")
	}
}
