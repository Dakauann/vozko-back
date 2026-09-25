package businessphone_usecase

import (
	"errors"
	"testing"

	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
)

type phoneListStub struct{ inputs []businessphone.ListInput }

func (l *phoneListStub) Execute(in businessphone.ListInput) (*shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber], error) {
	l.inputs = append(l.inputs, in)
	return &shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber]{}, nil
}

type phoneGrantsStub []string

func (g phoneGrantsStub) GetPhoneIDsForWorkspace(string) ([]string, error) { return g, nil }

func TestWorkspacePhonesListsOwnedAndGrantedPhonesOnly(t *testing.T) {
	list := &phoneListStub{}
	if _, err := NewWorkspacePhonesUseCase(list, phoneGrantsStub{"p-shared"}).List("ws1", businessphone.ListInput{OwnerWorkspaceID: "ws9"}); err != nil {
		t.Fatal(err)
	}
	in := list.inputs[0]
	if in.OwnerWorkspaceID != "ws1" || len(in.AccessPhoneIDs) != 1 || in.AccessPhoneIDs[0] != "p-shared" {
		t.Fatalf("input = %+v", in)
	}
}

func TestWorkspacePhonesNeverListsEveryPhone(t *testing.T) {
	list := &phoneListStub{}
	if _, err := NewWorkspacePhonesUseCase(list, nil).List(" ", businessphone.ListInput{}); !errors.Is(err, businessphone.ErrWorkspaceRequired) || len(list.inputs) != 0 {
		t.Fatalf("err %v lists %d", err, len(list.inputs))
	}
}
