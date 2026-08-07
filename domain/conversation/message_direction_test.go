package conversation

import "testing"

// Direction is a fact about a message that its TYPE cannot carry.
//
// The provider's own schema is explicit about this: its `messageType` is the
// "tipo de conteúdo da mensagem" and its `fromMe` flag is what says who sent it.
// Reading direction off the content type worked only for channels that agreed to
// lose the content type in exchange — Telegram and Instagram store every
// outbound message as `operator` whatever it contained. Unofficial WhatsApp
// keeps the honest type, so an owner replying on their own phone was stored as
// an ordinary text and read back as the CUSTOMER talking.

// The bug, stated at the level it actually lives: an outbound message whose
// content is a plain text must stay outbound.
func TestStatedDirectionSurvivesAnInboundLookingType(t *testing.T) {
	msg := &Message{
		MessageType: MessageTypeUserMessage,
		Direction:   MessageDirectionOutbound,
	}
	if got := msg.ResolvedDirection(); got != MessageDirectionOutbound {
		t.Fatalf("ResolvedDirection() = %q; the owner's reply was filed as the customer's", got)
	}
}

// The mirror: a stated INBOUND is not overridden by an outbound-looking type.
func TestStatedDirectionWinsBothWays(t *testing.T) {
	msg := &Message{
		MessageType: MessageTypeOperator,
		Direction:   MessageDirectionInbound,
	}
	if got := msg.ResolvedDirection(); got != MessageDirectionInbound {
		t.Errorf("ResolvedDirection() = %q, want INBOUND", got)
	}
}

// With nothing stated, the old inference stands — unchanged, so the callers that
// never passed a direction (the direct messageRepo.Create paths) behave exactly
// as they did.
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

// A row must never reach the database with no direction at all: an unstated one
// would push every reader back to the inference this column exists to delete.
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

// A nil message resolves to nothing rather than panicking or claiming a side.
func TestResolvedDirectionOnNil(t *testing.T) {
	var msg *Message
	if got := msg.ResolvedDirection(); got != MessageDirectionUnknown {
		t.Errorf("ResolvedDirection() = %q, want unknown", got)
	}
}

// The empty direction means "not stated", never "neither" and never a side.
// Readers branch on Valid, so a value that lied here would silently flip rows.
func TestDirectionValidity(t *testing.T) {
	cases := map[MessageHistoryDirection]bool{
		MessageDirectionInbound:  true,
		MessageDirectionOutbound: true,
		MessageDirectionUnknown:  false,
		"inbound":                false, // case matters; the column stores upper
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
