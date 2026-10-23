package node_executors

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	lead_domain "vozko/domain/lead"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workflow"
)

type answeredWindow struct {
	open bool
	err  error
	asks []string
}

func (w *answeredWindow) IsWindowOpen(leadID, businessPhoneID string) (bool, error) {
	w.asks = append(w.asks, leadID+"|"+businessPhoneID)
	return w.open, w.err
}

func (w *answeredWindow) GetLastMessageTime(string, string) (*time.Time, error) {
	return nil, w.err
}

type entryWithout struct {
	leadID  string
	phoneID string
}

func (e entryWithout) FindByID(id string) (*wce.WhatsAppCampaignEntry, error) {
	return &wce.WhatsAppCampaignEntry{ID: id, LeadID: e.leadID, ReceivedBusinessPhoneID: e.phoneID}, nil
}

func (e entryWithout) GetCampaignForEntry(string) (*wce.EntryCampaignInfo, error) {
	return &wce.EntryCampaignInfo{WorkspaceID: "ws-1"}, nil
}

func TestAFreeFormSendIsRefusedUnlessTheWindowIsProvenOpen(t *testing.T) {
	contact := &lead_domain.Lead{ID: "lead-1", WorkspaceID: "ws-1", Number: "5511999999999"}
	cases := []struct {
		name    string
		window  messageWindowLookup
		entries whatsappEntryLookup
		sends   bool
	}{
		{"an open window lets the message go", &answeredWindow{open: true}, stubMediaEntryRepo{}, true},
		{"a closed window refuses", &answeredWindow{open: false}, stubMediaEntryRepo{}, false},
		{"no window directory refuses instead of assuming open", nil, stubMediaEntryRepo{}, false},
		{"a conversation without a business number cannot prove the window", &answeredWindow{open: true}, entryWithout{leadID: "lead-1"}, false},
		{"a conversation without a lead cannot prove the window", &answeredWindow{open: true}, entryWithout{phoneID: "phone-1"}, false},
	}
	for _, tc := range cases {
		for kind, send := range everyWhatsAppSend(t) {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				client := &countingWhatsAppClient{}
				sender := guardedSender(contact, client, &recordingHistory{})
				sender.deps.WhatsAppEntryRepo = tc.entries
				sender.deps.MessageWindowRepo = tc.window
				err := send(sender)
				if tc.sends {
					if err != nil || client.sends != 1 {
						t.Fatalf("err = %v, sends = %d", err, client.sends)
					}
					return
				}
				if !errors.Is(err, conversation.ErrWindowClosed) || client.sends != 0 {
					t.Fatalf("err = %v, sends = %d, want a window refusal before any send", err, client.sends)
				}
				if workflow.ClassifyError(err) != workflow.ErrorKindWindowClosed || !strings.Contains(err.Error(), "template") {
					t.Fatalf("err = %v, want the window kind and the template fix", err)
				}
			})
		}
	}
}

func TestAnUnreadableWindowRefusesTheSendButLeavesItForARetry(t *testing.T) {
	contact := &lead_domain.Lead{ID: "lead-1", WorkspaceID: "ws-1", Number: "5511999999999"}
	for kind, send := range everyWhatsAppSend(t) {
		t.Run(kind, func(t *testing.T) {
			client := &countingWhatsAppClient{}
			sender := guardedSender(contact, client, &recordingHistory{})
			sender.deps.WhatsAppEntryRepo = stubMediaEntryRepo{}
			sender.deps.MessageWindowRepo = &answeredWindow{open: true, err: errors.New("db down")}
			err := send(sender)
			if err == nil || client.sends != 0 {
				t.Fatalf("err = %v, sends = %d, want a refusal before any send", err, client.sends)
			}
			if workflow.IsPermanentRefusal(err) {
				t.Fatalf("err = %v, want a read failure the engine retries, not a closed window", err)
			}
		})
	}
}
