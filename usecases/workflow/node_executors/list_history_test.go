package node_executors

import (
	"context"
	"reflect"
	"testing"

	"vozko/domain/conversation"
)

func TestAWorkflowListIsStoredWithItsButtonTextAndRowDescriptions(t *testing.T) {
	history := &recordingHistory{}
	sender := newMediaSender(interactiveSendClient{}, history, nil)
	input := conversation.SendListMessageInput{
		HeaderText: "Agenda", BodyText: "Escolha o horário", FooterText: "Responda pela lista", ButtonText: "Ver horários",
		Sections: []conversation.ListSection{{Title: "Tarde", Rows: []conversation.ListRow{{ID: "h14", Title: "14h", Description: "depois do almoço"}}}},
	}

	if _, _, err := sender.SendListWithInput(context.Background(), waMediaRun(), input); err != nil {
		t.Fatal(err)
	}

	if len(history.records) != 1 {
		t.Fatalf("records = %+v", history.records)
	}
	want := &conversation.InteractiveContent{
		Kind:        conversation.InteractiveKindList,
		Header:      &conversation.InteractiveHeader{Type: "text", Text: "Agenda"},
		Body:        "Escolha o horário",
		Footer:      "Responda pela lista",
		ButtonLabel: "Ver horários",
		Sections:    []conversation.InteractiveSection{{Title: "Tarde", Rows: []conversation.InteractiveRow{{ID: "h14", Title: "14h", Description: "depois do almoço"}}}},
	}
	if got := history.records[0].Interactive; !reflect.DeepEqual(got, want) {
		t.Fatalf("interactive = %+v, want %+v", got, want)
	}
}

func TestWorkflowReplyButtonsAreStoredWithTheirHeaderMedia(t *testing.T) {
	history := &recordingHistory{}
	sender := newMediaSender(interactiveSendClient{}, history, nil)
	input := conversation.SendButtonMessageInput{
		HeaderType: conversation.HeaderTypeImage, MediaLink: "https://cdn.exemplo.com/banner.jpg",
		BodyText: "Quer continuar?", FooterText: "Atendimento",
		Buttons: []conversation.InteractiveButton{{Type: conversation.ButtonTypeReply, ID: "sim", Title: "Sim"}, {Type: conversation.ButtonTypeCopyCode, CopyCode: "PROMO10"}},
	}

	if _, _, err := sender.SendButtonsWithInput(context.Background(), waMediaRun(), input); err != nil {
		t.Fatal(err)
	}

	want := &conversation.InteractiveContent{
		Kind:   conversation.InteractiveKindButtons,
		Header: &conversation.InteractiveHeader{Type: "image", MediaURL: "https://cdn.exemplo.com/banner.jpg"},
		Body:   "Quer continuar?",
		Footer: "Atendimento",
		Buttons: []conversation.InteractiveAction{
			{Kind: conversation.InteractiveActionReply, ID: "sim", Title: "Sim"},
			{Kind: conversation.InteractiveActionCopyCode, Code: "PROMO10"},
		},
	}
	if len(history.records) != 1 || !reflect.DeepEqual(history.records[0].Interactive, want) {
		t.Fatalf("records = %+v", history.records)
	}
}
