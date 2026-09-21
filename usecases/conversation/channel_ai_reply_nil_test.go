package conversation_usecase

import (
	"context"
	"testing"

	conversation "vozko/domain/conversation"
)

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
