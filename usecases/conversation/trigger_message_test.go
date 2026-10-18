package conversation_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type storedRows struct {
	byWamid    map[string]*conversation.Message
	byProvider map[string]*conversation.Message
	err        error
}

func (s storedRows) GetByWhatsAppMessageID(wamid string) (*conversation.Message, error) {
	if s.err != nil {
		return nil, s.err
	}
	if m, ok := s.byWamid[wamid]; ok {
		return m, nil
	}
	return nil, conversation.ErrMessageNotFound
}

func (s storedRows) GetByEntryAndExternalMessageID(entryType shared.EntryType, entryID, externalID string) (*conversation.Message, error) {
	if s.err != nil {
		return nil, s.err
	}
	if m, ok := s.byProvider[string(entryType)+"|"+entryID+"|"+externalID]; ok {
		return m, nil
	}
	return nil, conversation.ErrMessageNotFound
}

func TestTheTriggerMessageIsOurStoredRowNotTheProviderID(t *testing.T) {
	rows := storedRows{
		byWamid:    map[string]*conversation.Message{"wamid.in": {ID: "row-1"}},
		byProvider: map[string]*conversation.Message{"facebook|conv-1|mid.1": {ID: "row-2"}},
	}
	if got := StoredWhatsAppMessageID(rows, "wamid.in"); got != "row-1" {
		t.Fatalf("whatsapp = %q", got)
	}
	if got := StoredInboundMessageID(rows, shared.EntryTypeFacebook, "conv-1", "mid.1"); got != "row-2" {
		t.Fatalf("provider = %q", got)
	}
	for name, got := range map[string]string{
		"unknown wamid":    StoredWhatsAppMessageID(rows, "wamid.other"),
		"empty wamid":      StoredWhatsAppMessageID(rows, " "),
		"no lookup":        StoredWhatsAppMessageID(nil, "wamid.in"),
		"unknown provider": StoredInboundMessageID(rows, shared.EntryTypeFacebook, "conv-2", "mid.1"),
		"storage failure":  StoredInboundMessageID(storedRows{err: errors.New("down")}, shared.EntryTypeFacebook, "conv-1", "mid.1"),
		"no provider id":   StoredInboundMessageID(rows, shared.EntryTypeFacebook, "conv-1", ""),
		"no provider repo": StoredInboundMessageID(nil, shared.EntryTypeFacebook, "conv-1", "mid.1"),
	} {
		if got != "" {
			t.Errorf("%s: got %q", name, got)
		}
	}
}

type countingRows struct {
	storedRows
	count int64
}

func (c countingRows) CountInboundByEntry(string, shared.EntryType) (int64, error) {
	return c.count, nil
}

func TestInboundAutomationNamesTheStoredMessageThatTriggeredTheWorkflow(t *testing.T) {
	ev := &recordingEvaluator{}
	rows := countingRows{count: 1, storedRows: storedRows{byProvider: map[string]*conversation.Message{"telegram|e-1|tg:1": {ID: "row-9"}}}}
	auto := NewInboundAutomation(ev, &recordingReplier{}, &recordingScheduler{}, rows)
	in := automationInput()
	in.ProviderMessageID = "tg:1"

	auto.FireWorkflows(context.Background(), in)

	if len(ev.events) != 2 {
		t.Fatalf("events = %+v", ev.events)
	}
	for _, e := range ev.events {
		if e.Data[workflow.DataKeyTriggerMessageID] != "row-9" {
			t.Fatalf("%s data = %+v", e.TriggerType, e.Data)
		}
	}
}
