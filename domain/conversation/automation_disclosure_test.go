package conversation

import (
	"testing"
	"time"
)

func TestAutomationDisclosure(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	msg := func(kind MessageType, ago time.Duration) *Message {
		return &Message{MessageType: kind, CreatedAt: now.Add(-ago)}
	}
	const disclosure = "Você está falando com um assistente virtual."

	cases := []struct {
		name      string
		history   []*Message
		disclose  string
		wantAdded bool
	}{
		{"first bot reply", []*Message{msg(MessageTypeUserMessage, time.Minute)}, disclosure, true},
		{"bot replied an hour ago", []*Message{msg(MessageTypeUserMessage, time.Minute), msg(MessageTypeAIResponse, time.Hour)}, disclosure, false},
		{"bot replied two days ago", []*Message{msg(MessageTypeUserMessage, time.Minute), msg(MessageTypeAIResponse, 48 * time.Hour)}, disclosure, true},
		{"human spoke after the bot", []*Message{msg(MessageTypeUserMessage, time.Minute), msg(MessageTypeOperator, 10 * time.Minute), msg(MessageTypeAIResponse, time.Hour)}, disclosure, true},
		{"channel needs no disclosure", []*Message{msg(MessageTypeUserMessage, time.Minute)}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := WithAutomationDisclosure(tc.disclose, "Claro!", tc.history, now)
			want := "Claro!"
			if tc.wantAdded {
				want = disclosure + "\n\nClaro!"
			}
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}
