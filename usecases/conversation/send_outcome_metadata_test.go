package conversation_usecase

import (
	"context"
	"encoding/json"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type outcomeAdapter struct {
	campaignSendAdapter
	outcome *conversation.SendOutcome
}

func (a *outcomeAdapter) SendText(context.Context, *conversation.EntryContext, conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	return a.outcome, nil
}

func TestTheProviderOutcomeTravelsIntoTheStoredMessage(t *testing.T) {
	adapter := &outcomeAdapter{
		campaignSendAdapter: campaignSendAdapter{entryType: shared.EntryTypeMercadoLivre},
		outcome: &conversation.SendOutcome{
			ProviderMessageID: "prov-1",
			DeliveryStatus:    conversation.DeliveryStatusFailed,
			Metadata:          map[string]any{"mercadolivre_moderation": map[string]any{"status": "rejected", "reason": "PERSONAL_DATA"}},
		},
	}
	msgs := &campaignMessageRepo{}
	svc := &MessageSenderService{messageRepo: msgs, channelAdapters: conversation.NewAdapterRegistry(adapter)}

	if _, err := svc.SendTextMessage("conv-1", string(shared.EntryTypeMercadoLivre), "oi", conversation.SendOrigin{SenderUserID: "u-1"}); err != nil {
		t.Fatalf("SendTextMessage: %v", err)
	}
	if len(msgs.created) != 1 {
		t.Fatalf("created %d messages", len(msgs.created))
	}
	stored := msgs.created[0]
	if stored.DeliveryStatus != conversation.DeliveryStatusFailed {
		t.Errorf("delivery status = %q, want the provider's verdict", stored.DeliveryStatus)
	}
	var meta map[string]map[string]string
	if err := json.Unmarshal(stored.Metadata, &meta); err != nil || meta["mercadolivre_moderation"]["reason"] != "PERSONAL_DATA" {
		t.Fatalf("metadata = %s (%v)", stored.Metadata, err)
	}
}

func TestAnOutcomeWithoutAVerdictKeepsTheSentStatus(t *testing.T) {
	adapter := &outcomeAdapter{
		campaignSendAdapter: campaignSendAdapter{entryType: shared.EntryTypeMercadoLivre},
		outcome:             &conversation.SendOutcome{ProviderMessageID: "prov-1"},
	}
	msgs := &campaignMessageRepo{}
	svc := &MessageSenderService{messageRepo: msgs, channelAdapters: conversation.NewAdapterRegistry(adapter)}
	if _, err := svc.SendTextMessage("conv-1", string(shared.EntryTypeMercadoLivre), "oi", conversation.SendOrigin{SenderUserID: "u-1"}); err != nil {
		t.Fatalf("SendTextMessage: %v", err)
	}
	if got := msgs.created[0]; got.DeliveryStatus != conversation.DeliveryStatusSent || len(got.Metadata) != 0 {
		t.Fatalf("message = %+v", got)
	}
}
