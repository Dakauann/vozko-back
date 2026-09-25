package conversation_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type sendRecorder struct{ inputs []conversation.OperatorSendInput }

func (s *sendRecorder) Execute(_ context.Context, in conversation.OperatorSendInput) (*conversation.Message, error) {
	s.inputs = append(s.inputs, in)
	return &conversation.Message{ID: "m1", Text: in.Text}, nil
}

func TestPersonSendRefusesAConversationThePersonCannotSee(t *testing.T) {
	send := &sendRecorder{}
	_, err := NewPersonSend(&accessStub{allow: false}, send).Execute(context.Background(), shared.Person{UserID: "u1"},
		conversation.OperatorSendInput{EntryID: "e1", EntryType: "whatsapp", WorkspaceID: "ws1", Text: "oi"})
	if !errors.Is(err, conversation.ErrUnauthorized) || len(send.inputs) != 0 {
		t.Fatalf("err %v sends %d", err, len(send.inputs))
	}
}

func TestPersonSendRefusesEntriesWithoutConversationVisibility(t *testing.T) {
	send := &sendRecorder{}
	_, err := NewPersonSend(&accessStub{allow: true}, send).Execute(context.Background(), shared.Person{UserID: "u1"},
		conversation.OperatorSendInput{EntryID: "e1", EntryType: "fax", WorkspaceID: "ws1", Text: "oi"})
	if !errors.Is(err, conversation.ErrEntryTypeInvalid) || len(send.inputs) != 0 {
		t.Fatalf("err %v sends %d", err, len(send.inputs))
	}
}

func TestPersonSendSendsAsThePerson(t *testing.T) {
	send := &sendRecorder{}
	if _, err := NewPersonSend(&accessStub{allow: true}, send).Execute(context.Background(), shared.Person{UserID: "u1"},
		conversation.OperatorSendInput{EntryID: "e1", EntryType: "whatsapp", WorkspaceID: "ws1", Text: "oi", SenderUserID: "forged"}); err != nil {
		t.Fatal(err)
	}
	if send.inputs[0].SenderUserID != "u1" {
		t.Fatalf("sender = %q", send.inputs[0].SenderUserID)
	}
}
