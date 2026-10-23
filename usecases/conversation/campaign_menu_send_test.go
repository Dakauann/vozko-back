package conversation_usecase

import (
	"context"
	"testing"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type campaignMenuAdapter struct {
	campaignSendAdapter
	sent []conversation.SendInteractiveRequest
}

func (a *campaignMenuAdapter) SendInteractive(_ context.Context, _ *conversation.EntryContext, req conversation.SendInteractiveRequest) (*conversation.SendOutcome, error) {
	a.sent = append(a.sent, req)
	return conversation.ShowingOptions(&conversation.SendOutcome{ProviderMessageID: "menu-1"}, req.ShowingAs(channel.InteractiveStyleList, req.Options[:2])), nil
}

func (a *campaignMenuAdapter) InteractiveLimits() channel.InteractiveLimits {
	return channel.InteractiveLimits{MaxOptionsList: 2}
}

func TestACampaignMenuIsStoredWithTheRowsTheContactReceived(t *testing.T) {
	adapter := &campaignMenuAdapter{campaignSendAdapter: campaignSendAdapter{entryType: shared.EntryTypeUnofficialWhatsApp}}
	msgs := &campaignMessageRepo{}
	svc := &MessageSenderService{messageRepo: msgs, channelAdapters: conversation.NewAdapterRegistry(adapter)}

	message, err := svc.SendCampaignMessage(SendCampaignMessageInput{
		CampaignID: "camp-1", EntryID: "conv-1", EntryType: string(shared.EntryTypeUnofficialWhatsApp),
		Text: "Escolha", Footer: "Rodapé", Style: channel.InteractiveStyleList, ListButton: "Ver planos",
		Options: []conversation.InteractiveOption{{ID: "a", Title: "Plano A", Description: "mensal"}, {ID: "b", Title: "Plano B"}, {ID: "c", Title: "Plano C"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if adapter.sent[0].ListButton != "Ver planos" {
		t.Fatalf("request = %+v", adapter.sent[0])
	}
	content, ok := message.Interactive()
	if !ok || content.Kind != conversation.InteractiveKindList || content.ButtonLabel != "Ver planos" || len(content.Sections[0].Rows) != 2 || content.Sections[0].Rows[0].Description != "mensal" {
		t.Fatalf("content = %+v, %v", content, ok)
	}
	if len(msgs.created) != 1 || msgs.created[0] != message {
		t.Fatalf("created = %+v", msgs.created)
	}
}
