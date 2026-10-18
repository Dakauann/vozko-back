package conversation_event

import "testing"

func TestLeadExitLinesAreKnownConversationEvents(t *testing.T) {
	cases := map[EventType]string{
		EventLeadExitAction:  "lead_exit_action",
		EventLeadSendRefused: "lead_send_refused",
	}
	for eventType, want := range cases {
		if string(eventType) != want || !eventType.Valid() {
			t.Errorf("%q: valid = %v, want %q", eventType, eventType.Valid(), want)
		}
	}
}
