package node_executors

import (
	"testing"

	"vozko/domain/ai"
	"vozko/domain/conversation"
)

func TestComposeHistoryGivesTheCustomerRoleOnlyToTheContact(t *testing.T) {
	history := []*conversation.Message{
		{Text: "oi", MessageType: conversation.MessageTypeUserMessage, SentBy: conversation.SentByContact("5511")},
		{Text: "posso ajudar", MessageType: conversation.MessageTypeOperator, SentBy: conversation.SentByPerson("user-1")},
		{Text: "catalogo.pdf", MessageType: conversation.MessageTypeMedia, SentBy: conversation.SentByWorkflow("wf-1")},
		{Text: "enviado do celular", MessageType: conversation.MessageTypeUserMessage, SentBy: conversation.SentExternally()},
	}

	got := composeHistory(history, 10)

	want := []ai.Role{ai.RoleUser, ai.RoleAssistant, ai.RoleAssistant, ai.RoleAssistant}
	if len(got) != len(want) {
		t.Fatalf("got %d turns, want %d: %+v", len(got), len(want), got)
	}
	for i, role := range want {
		if got[i].Role != role {
			t.Errorf("turn %d %q is %s, want %s", i, got[i].Content, got[i].Role, role)
		}
	}
}
