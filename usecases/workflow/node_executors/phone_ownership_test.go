package node_executors

import (
	"testing"

	businessphone "vozko/domain/whatsapp/business_phone"
)

type phonesOwnedBy map[string]string

func (p phonesOwnedBy) FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	owner, ok := p[id]
	if !ok {
		return nil, nil
	}
	return &businessphone.WhatsAppBusinessPhoneNumber{ID: id, OwnerWorkspaceID: owner}, nil
}

func TestAChosenSenderPhoneMustBeProvedToBelongToTheWorkspace(t *testing.T) {
	cases := []struct {
		name      string
		phones    businessPhoneLookup
		workspace string
		wantErr   bool
	}{
		{"own phone", phonesOwnedBy{"bp-1": "ws-1"}, "ws-1", false},
		{"another workspace's phone", phonesOwnedBy{"bp-1": "ws-2"}, "ws-1", true},
		{"unknown phone", phonesOwnedBy{}, "ws-1", true},
		{"no phone directory", nil, "ws-1", true},
		{"no workspace", phonesOwnedBy{"bp-1": "ws-1"}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sender := &whatsappSender{deps: SenderDeps{BusinessPhoneRepo: tc.phones}}
			err := sender.ensureWorkspaceOwnsPhone(tc.workspace, "bp-1")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
