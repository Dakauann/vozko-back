package facebook

import (
	"context"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	conversation_usecase "vozko/usecases/conversation"
	"vozko/usecases/metachannel"
)

type storedByProvider map[string]string

func (s storedByProvider) CountInboundByEntry(string, shared.EntryType) (int64, error) { return 2, nil }

func (s storedByProvider) GetByEntryAndExternalMessageID(entryType shared.EntryType, _ string, externalID string) (*conversation.Message, error) {
	if id, ok := s[string(entryType)+"|"+externalID]; ok {
		return &conversation.Message{ID: id}, nil
	}
	return nil, conversation.ErrMessageNotFound
}

func TestTheWorkflowRunNamesTheStoredInboundMessage(t *testing.T) {
	f := newMessagingFixture()
	transcript := &metachannel.Transcript{
		EntryType: shared.EntryTypeFacebook, Channel: conversation.MessageChannelFacebook, Prefix: "facebook", History: f.history, Ads: f.ads,
	}
	f.uc = NewHandleMessagingUseCase(HandleMessagingDeps{
		Pages: f.pages, Contacts: f.contacts, Conversations: f.convs, Profiles: f.profiles,
		Transcript: transcript, Watermarks: f.watermarks, Assignments: f.assigns,
		Automation: conversation_usecase.NewInboundAutomation(f.workflows, f.agents, nil, storedByProvider{"facebook|m7": "row-7"}),
		OurAppID:   ourAppID,
	})

	if err := f.uc.Execute(context.Background(), entry(inbound("m7", "oi"))); err != nil {
		t.Fatal(err)
	}

	if len(f.workflows.events) == 0 || f.workflows.events[0].Data[workflow.DataKeyTriggerMessageID] != "row-7" {
		t.Fatalf("events = %+v", f.workflows.events)
	}
}
