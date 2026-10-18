package conversation

import "testing"

func TestLastMessageOrigin(t *testing.T) {
	cases := []struct {
		name  string
		entry EntryWithLastMessage
		want  LastMessageOrigin
	}{
		{"group member", EntryWithLastMessage{LastMessageSenderKind: "contact", LastMessageSenderID: "+5511900000001", LastMessageType: MessageTypeUserMessage}, LastMessageByContact},
		{"operator", EntryWithLastMessage{LastMessageSenderKind: "human", LastMessageSenderID: "8a1f3c2e-6b7d-4e5f-9a0b-1c2d3e4f5a6b", LastMessageType: MessageTypeOperator}, LastMessageByBusiness},
		{"sent from the phone", EntryWithLastMessage{LastMessageSenderKind: "external", LastMessageType: MessageTypeUserMessage}, LastMessageByBusiness},
		{"agent", EntryWithLastMessage{LastMessageSenderKind: "ai", LastMessageSenderID: "ai:agent-1", LastMessageType: MessageTypeAIResponse}, LastMessageByBusiness},
		{"system notice", EntryWithLastMessage{LastMessageSenderKind: "system", LastMessageType: MessageTypeSystem}, LastMessageByNobody},
		{"legacy inbound row", EntryWithLastMessage{LastMessageSenderKind: "unknown", LastMessageType: MessageTypeAudio}, LastMessageByContact},
		{"legacy operator row", EntryWithLastMessage{LastMessageSenderKind: "unknown", LastMessageType: MessageTypeOperator}, LastMessageByBusiness},
		{"legacy system row", EntryWithLastMessage{LastMessageType: MessageTypeSystem}, LastMessageByNobody},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.entry.LastMessageOrigin(); got != tc.want {
				t.Errorf("origin = %q, want %q", got, tc.want)
			}
		})
	}
}
