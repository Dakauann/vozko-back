package conversation_event

import "testing"

func TestAgentReplyWithheldIsAKnownConversationEvent(t *testing.T) {
	if EventAgentReplyWithheld != "agent_reply_withheld" || !EventAgentReplyWithheld.Valid() {
		t.Fatalf("%q valid = %v", EventAgentReplyWithheld, EventAgentReplyWithheld.Valid())
	}
}
