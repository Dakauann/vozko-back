package conversation_usecase

import (
	"testing"

	"vozko/domain/conversation"
)

func msg(t conversation.MessageType) *conversation.Message {
	return &conversation.Message{MessageType: t}
}

// TestIsFirstInboundMessage guards the trigger_first_message fix: a template-first
// campaign records an outbound template before the lead replies, so the reply must
// still count as the first customer message. Only a PRIOR inbound message
// suppresses the trigger.
func TestIsFirstInboundMessage(t *testing.T) {
	cases := []struct {
		name    string
		history []*conversation.Message
		want    bool
	}{
		{"empty history (organic inbound-first)", nil, true},
		{
			// The incident: outbound template in history, lead's first reply.
			"only outbound template", []*conversation.Message{msg(conversation.MessageTypeTemplate)}, true,
		},
		{
			"outbound template + agent reply (all outbound)",
			[]*conversation.Message{msg(conversation.MessageTypeTemplate), msg(conversation.MessageTypeAIResponse)},
			true,
		},
		{
			"prior inbound text → not first",
			[]*conversation.Message{msg(conversation.MessageTypeUserMessage)},
			false,
		},
		{
			"template then prior inbound reply → not first",
			[]*conversation.Message{msg(conversation.MessageTypeTemplate), msg(conversation.MessageTypeUserMessage)},
			false,
		},
		{
			"prior inbound audio → not first",
			[]*conversation.Message{msg(conversation.MessageTypeAudio)},
			false,
		},
		{"nil element is ignored", []*conversation.Message{nil, msg(conversation.MessageTypeTemplate)}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isFirstInboundMessage(tc.history); got != tc.want {
				t.Fatalf("isFirstInboundMessage=%v, want %v", got, tc.want)
			}
		})
	}
}
