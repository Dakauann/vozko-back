package conversation_usecase

import (
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type templateSenderStub struct{ calls []string }

func (s *templateSenderStub) SendTemplate(entryID, entryType, templateID string, parameters []string, userID string, workspaceID string) (string, error) {
	s.calls = append(s.calls, userID+"|"+workspaceID+"|"+entryID+"|"+templateID)
	return "msg-1", nil
}

func TestPersonTemplateSendRefusesAConversationThePersonCannotSee(t *testing.T) {
	sender := &templateSenderStub{}
	_, err := NewPersonTemplateSend(&accessStub{allow: false}, sender).Execute(shared.Person{UserID: "u1"},
		conversation.TemplateSendRequest{WorkspaceID: "ws1", EntryID: "e1", EntryType: "whatsapp", TemplateID: "t1"})
	if !errors.Is(err, conversation.ErrUnauthorized) || len(sender.calls) != 0 {
		t.Fatalf("err %v calls %v", err, sender.calls)
	}
}

func TestPersonTemplateSendSendsAsThePerson(t *testing.T) {
	sender := &templateSenderStub{}
	if _, err := NewPersonTemplateSend(&accessStub{allow: true}, sender).Execute(shared.Person{UserID: "u1"},
		conversation.TemplateSendRequest{WorkspaceID: "ws1", EntryID: "e1", EntryType: "whatsapp", TemplateID: "t1"}); err != nil {
		t.Fatal(err)
	}
	if sender.calls[0] != "u1|ws1|e1|t1" {
		t.Fatalf("calls %v", sender.calls)
	}
}
