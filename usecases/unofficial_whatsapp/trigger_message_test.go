package unofficial_whatsapp

import (
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type storedInbound struct {
	conversation.MessageRepository
	byProvider map[string]string
}

func (s storedInbound) GetByEntryAndExternalMessageID(entryType shared.EntryType, _ string, externalID string) (*conversation.Message, error) {
	if id, ok := s.byProvider[string(entryType)+"|"+externalID]; ok {
		return &conversation.Message{ID: id}, nil
	}
	return nil, conversation.ErrMessageNotFound
}

func TestTheWorkflowRunNamesTheStoredInboundMessage(t *testing.T) {
	h := newAutoHarness(t, nil)
	h.uc.messages = storedInbound{byProvider: map[string]string{"unofficial_whatsapp|msg-1": "row-1"}}

	h.deliverInbound(t)

	events := h.workflows.all()
	if len(events) == 0 || events[0].Data[workflow.DataKeyTriggerMessageID] != "row-1" {
		t.Fatalf("events = %+v", events)
	}
}
