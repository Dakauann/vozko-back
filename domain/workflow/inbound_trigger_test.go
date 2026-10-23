package workflow

import (
	"reflect"
	"testing"

	"vozko/domain/conversation"
)

func TestEveryChannelGetsTheSameMessageVariables(t *testing.T) {
	for name, tc := range map[string]struct {
		media     conversation.MediaType
		kind      string
		mediaType interface{}
	}{
		"text":     {"", InboundKindText, nil},
		"image":    {conversation.MediaTypeImage, InboundKindMedia, "image"},
		"document": {conversation.MediaTypeDocument, InboundKindMedia, "document"},
		"audio":    {conversation.MediaTypeAudio, InboundKindAudio, "audio"},
	} {
		event := InboundTrigger{WorkspaceID: "ws-1", EntryID: "e-1", EntryType: "telegram", Text: "Ana", Media: tc.media}.Event()

		if event.TriggerType != TriggerMessageReceived || event.Data[DataKeyMessage] != "Ana" || event.Data[DataKeyMessageType] != tc.kind ||
			event.Data[DataKeyMediaType] != tc.mediaType || event.Data[DataKeyChannel] != "telegram" || event.Data[DataKeyWorkspaceID] != "ws-1" {
			t.Fatalf("%s: data = %+v", name, event.Data)
		}
		if _, legacy := event.Data["text"]; legacy {
			t.Fatalf("%s: the reply is only ever called message", name)
		}
	}
}

func TestTheWorkflowScopeFollowsTheCampaign(t *testing.T) {
	account := InboundTrigger{Text: "oi", WorkflowID: "wf-1"}.Event().Data
	campaign := InboundTrigger{Text: "oi", WorkflowID: "wf-1", CampaignID: "c-1"}.Event().Data

	if account[DataKeyAccountWorkflowID] != "wf-1" || account[DataKeyCampaignWorkflowID] != nil {
		t.Fatalf("account = %+v", account)
	}
	if campaign[DataKeyCampaignWorkflowID] != "wf-1" || campaign[DataKeyCampaignID] != "c-1" || campaign[DataKeyAccountWorkflowID] != nil {
		t.Fatalf("campaign = %+v", campaign)
	}
}

func TestTheStoredMessageNamesTheTriggerAndItsMedia(t *testing.T) {
	trigger := InboundTrigger{Text: "foto"}
	trigger.StoredAs(&conversation.Message{ID: "row-1", MediaType: conversation.MediaTypeImage})
	data := trigger.Event().Data

	if data[DataKeyTriggerMessageID] != "row-1" || data[DataKeyMessageType] != InboundKindMedia || data[DataKeyMediaType] != "image" {
		t.Fatalf("data = %+v", data)
	}
}

func TestTheFirstMessageIsTheSameEventWithoutTheReplyRoute(t *testing.T) {
	event := InboundTrigger{Text: "oi"}.Event()
	event.Reply = &ReplyRouting{}

	first := event.AsFirstMessage()

	if first.TriggerType != TriggerFirstMessage || first.Reply != nil || !reflect.DeepEqual(first.Data, event.Data) {
		t.Fatalf("first = %+v", first)
	}
}
