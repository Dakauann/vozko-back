package unofficial_whatsapp

import (
	"context"
	"testing"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

type enrichmentRecorder struct{ calls int }

func (s *enrichmentRecorder) ScheduleAnalysis(string, shared.EntryType) { s.calls++ }

func TestCampaignControlsEnrichmentScheduling(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		scheduler := &enrichmentRecorder{}
		uc := &HandleWebhookUseCase{analysis: scheduler}
		instance := &uw.Instance{EnableAnalysis: !enabled, EnableAutoStaging: !enabled, EnableAutoMemory: !enabled}
		uc.scheduleAnalysis(instance, &uw.Conversation{ID: "conv"}, &CampaignAutomation{EnableAnalysis: enabled, EnableAutoMemory: enabled, EnableAutoStaging: enabled})
		if (scheduler.calls == 1) != enabled {
			t.Fatalf("campaign enabled=%v calls=%d", enabled, scheduler.calls)
		}
	}
}

type receiptMessages struct {
	conversation.MessageRepository
	updated bool
}

func (r *receiptMessages) UpdateDeliveryStatus(string, conversation.DeliveryStatus) error {
	r.updated = true
	return nil
}

func TestReceiptWithoutChatDoesNotQueryConversations(t *testing.T) {
	messages := &receiptMessages{}
	uc := &HandleWebhookUseCase{messages: messages}
	err := uc.handleMessageUpdate(context.Background(), &uw.Instance{ID: "inst"}, &uw.Event{ProviderMessageID: "message", DeliveryStatus: uw.DeliveryRead})
	if err != nil || !messages.updated {
		t.Fatalf("receipt not persisted: %v", err)
	}
}
