package conversation

import "testing"

func TestStatedDirectionSurvivesAnInboundLookingType(t *testing.T) {
	msg := &Message{
		MessageType: MessageTypeUserMessage,
		Direction:   MessageDirectionOutbound,
	}
	if got := msg.ResolvedDirection(); got != MessageDirectionOutbound {
		t.Fatalf("ResolvedDirection() = %q; the owner's reply was filed as the customer's", got)
	}
}

func TestStatedDirectionWinsBothWays(t *testing.T) {
	msg := &Message{
		MessageType: MessageTypeOperator,
		Direction:   MessageDirectionInbound,
	}
	if got := msg.ResolvedDirection(); got != MessageDirectionInbound {
		t.Errorf("ResolvedDirection() = %q, want INBOUND", got)
	}
}

func TestUnstatedDirectionFallsBackToTheType(t *testing.T) {
	cases := map[MessageType]MessageHistoryDirection{
		MessageTypeUserMessage:  MessageDirectionInbound,
		MessageTypeAudio:        MessageDirectionInbound,
		MessageTypeMedia:        MessageDirectionInbound,
		MessageTypeStoryReply:   MessageDirectionInbound,
		MessageTypeStoryMention: MessageDirectionInbound,
		MessageTypeOperator:     MessageDirectionOutbound,
		MessageTypeAIResponse:   MessageDirectionOutbound,
		MessageTypeTemplate:     MessageDirectionOutbound,
		MessageTypeToolCall:     MessageDirectionOutbound,
		MessageTypeToolResult:   MessageDirectionOutbound,
		MessageTypeSystem:       MessageDirectionOutbound,
	}
	for msgType, want := range cases {
		t.Run(string(msgType), func(t *testing.T) {
			msg := &Message{MessageType: msgType}
			if got := msg.ResolvedDirection(); got != want {
				t.Errorf("ResolvedDirection() = %q, want %q", got, want)
			}
		})
	}
}

func TestResolvedDirectionIsNeverEmpty(t *testing.T) {
	for _, msg := range []*Message{
		{},
		{MessageType: MessageTypeUnsupported},
		{MessageType: "something_added_later"},
		{Direction: "GARBAGE"},
	} {
		if got := msg.ResolvedDirection(); !got.Valid() {
			t.Errorf("MessageType=%q Direction=%q resolved to %q", msg.MessageType, msg.Direction, got)
		}
	}
}

func TestResolvedDirectionOnNil(t *testing.T) {
	var msg *Message
	if got := msg.ResolvedDirection(); got != MessageDirectionUnknown {
		t.Errorf("ResolvedDirection() = %q, want unknown", got)
	}
}

func TestDirectionValidity(t *testing.T) {
	cases := map[MessageHistoryDirection]bool{
		MessageDirectionInbound:  true,
		MessageDirectionOutbound: true,
		MessageDirectionUnknown:  false,
		"inbound":                false,
		"OUT":                    false,
	}
	for direction, want := range cases {
		if got := direction.Valid(); got != want {
			t.Errorf("%q.Valid() = %v, want %v", direction, got, want)
		}
	}
	if MessageDirectionUnknown.IsOutbound() {
		t.Error("an unstated direction reported itself outbound")
	}
	if MessageDirectionInbound.IsOutbound() {
		t.Error("INBOUND reported itself outbound")
	}
}
