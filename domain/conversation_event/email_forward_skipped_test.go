package conversation_event

import "testing"

func TestEmailForwardSkippedIsAValidEvent(t *testing.T) {
	if EventEmailForwardSkipped != "email_forward_skipped" || !EventEmailForwardSkipped.Valid() {
		t.Fatalf("%q valid = %v", EventEmailForwardSkipped, EventEmailForwardSkipped.Valid())
	}
}
