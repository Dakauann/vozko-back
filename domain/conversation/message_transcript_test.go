package conversation

import "testing"

func TestFromCustomerFollowsTheResolvedDirection(t *testing.T) {
	cases := []struct {
		name string
		msg  *Message
		want bool
	}{
		{"inbound", &Message{MessageType: MessageTypeUserMessage, Direction: MessageDirectionInbound}, true},
		{"owner reply typed as user", &Message{MessageType: MessageTypeUserMessage, Direction: MessageDirectionOutbound}, false},
		{"legacy inbound type", &Message{MessageType: MessageTypeUserMessage}, true},
		{"legacy operator", &Message{MessageType: MessageTypeOperator}, false},
		{"ai reply", &Message{MessageType: MessageTypeAIResponse}, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := c.msg.FromCustomer(); got != c.want {
			t.Errorf("%s: FromCustomer() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTranscribableKeepsOnlyWhatPeopleSaid(t *testing.T) {
	cases := []struct {
		name string
		msg  *Message
		want bool
	}{
		{"customer text", &Message{MessageType: MessageTypeUserMessage, Text: "oi"}, true},
		{"operator text", &Message{MessageType: MessageTypeOperator, Text: "olá"}, true},
		{"blank text", &Message{MessageType: MessageTypeUserMessage, Text: "  "}, false},
		{"tool call", &Message{MessageType: MessageTypeToolCall, Text: "{}"}, false},
		{"tool result", &Message{MessageType: MessageTypeToolResult, Text: "{}"}, false},
		{"system", &Message{MessageType: MessageTypeSystem, Text: "closed"}, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := c.msg.Transcribable(); got != c.want {
			t.Errorf("%s: Transcribable() = %v, want %v", c.name, got, c.want)
		}
	}
}
