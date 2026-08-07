package conversation_usecase

import (
	"context"
	"testing"

	conversation "vozko/domain/conversation"
)

// A nil *ChannelAIReplyService must answer "no reply", not panic.
//
// This is the regression test for a live outage: the container built this
// service AFTER wiring the channels that consume it, so each channel stored a
// nil pointer inside its own AIReplier interface. A nil pointer in an interface
// is NOT == nil, so every channel's `if uc.aiReply == nil { return }` guard
// passed, and the first inbound WhatsApp message with an agent assigned panicked
// inside the handler goroutine.
//
// The ordering is fixed in the container, and this keeps the failure mode from
// ever being a panic again: a future channel wired in the wrong order loses its
// AI replies, which is visible and survivable, instead of dropping messages.
func TestReplyOnNilServiceDoesNotPanic(t *testing.T) {
	var service *ChannelAIReplyService

	msg, err := service.Reply(context.Background(), conversation.AIReplyRequest{
		WorkspaceID:           "ws-1",
		EntryID:               "entry-1",
		EntryType:             "unofficial_whatsapp",
		AgentID:               "agent-1",
		AgentResponsesEnabled: true,
		Text:                  "oi",
	})
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if msg != nil {
		t.Errorf("message = %+v, want nil", msg)
	}
}

// The same call through the interface every channel actually holds, which is
// where the typed-nil trap lives. Without the nil-receiver guard this panics
// while `replier != nil` is true.
func TestNilServiceBehindAnInterfaceDoesNotPanic(t *testing.T) {
	var service *ChannelAIReplyService
	var replier interface {
		Reply(context.Context, conversation.AIReplyRequest) (*conversation.Message, error)
	} = service

	if replier == nil {
		t.Fatal("a nil pointer in an interface compares non-nil; this test is meaningless otherwise")
	}
	if _, err := replier.Reply(context.Background(), conversation.AIReplyRequest{AgentID: "a"}); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}
