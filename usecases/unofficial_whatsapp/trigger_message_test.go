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
	inbound    int64
}

func (s storedInbound) CountInboundByEntry(string, shared.EntryType) (int64, error) {
	return s.inbound, nil
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

func TestTheFirstInboundMessageAlsoFiresTheFirstMessageTrigger(t *testing.T) {
	for name, tc := range map[string]struct {
		inbound int64
		want    []workflow.TriggerType
	}{
		"first message":  {1, []workflow.TriggerType{workflow.TriggerMessageReceived, workflow.TriggerFirstMessage}},
		"later messages": {2, []workflow.TriggerType{workflow.TriggerMessageReceived}},
	} {
		h := newAutoHarness(t, nil)
		h.uc.messages = storedInbound{inbound: tc.inbound}

		h.deliverInbound(t)

		events := h.workflows.all()
		if len(events) != len(tc.want) {
			t.Fatalf("%s: events = %+v", name, events)
		}
		for i, event := range events {
			if event.TriggerType != tc.want[i] || event.Data["account_workflow_id"] != "inst-workflow" {
				t.Fatalf("%s: event %d = %+v", name, i, event)
			}
		}
	}
}
