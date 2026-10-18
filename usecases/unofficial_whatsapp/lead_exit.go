package unofficial_whatsapp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/leadexit"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

var errLeadExitStepMissing = errors.New("unofficial whatsapp: the lead exit step is not wired, the message cannot be checked for an exit")

func (uc *HandleWebhookUseCase) SetLeadExitStep(step leadexit.Step) {
	uc.exitStep = step
}

func (uc *HandleWebhookUseCase) runLeadExit(ctx context.Context, instance *uw.Instance, sub *chatContext, ev *uw.Event) (leadexit.Result, error) {
	leadID := exitLeadOf(sub)
	if leadID == "" {
		return leadexit.Result{}, nil
	}
	if uc.exitStep == nil {
		return leadexit.Result{}, errLeadExitStepMissing
	}
	in := leadexit.Inbound{
		WorkspaceID:      instance.WorkspaceID,
		LeadID:           leadID,
		SenderNumber:     uw.PhoneFromJID(sub.conversation.ChatID),
		EntryID:          sub.conversation.ID,
		EntryType:        string(shared.EntryTypeUnofficialWhatsApp),
		Channel:          string(shared.EntryTypeUnofficialWhatsApp),
		MessageID:        ev.ProviderMessageID,
		TriggerMessageID: uc.storedInboundID(sub.conversation.ID, ev.ProviderMessageID),
		Text:             ev.Text,
		ReceivedAt:       ev.Timestamp,
	}
	result, err := uc.exitStep.Run(ctx, in, &uazapiExitReplier{uc: uc, entryID: sub.conversation.ID})
	if err != nil {
		return result, fmt.Errorf("lead exit of %s: %w", ev.ProviderMessageID, err)
	}
	return result, nil
}

func exitLeadOf(sub *chatContext) string {
	if sub == nil || sub.conversation == nil || sub.conversation.IsGroup || sub.subject == nil || sub.subject.LeadID == nil {
		return ""
	}
	return strings.TrimSpace(*sub.subject.LeadID)
}

type uazapiExitReplier struct {
	uc      *HandleWebhookUseCase
	entryID string
}

func (r *uazapiExitReplier) Reply(ctx context.Context, text string) error {
	adapter := NewChannelAdapter(r.uc.instances, r.uc.servers, r.uc.contacts, r.uc.conversations, r.uc.messaging)
	ec, err := adapter.ResolveEntry(ctx, r.entryID)
	if err != nil {
		return err
	}
	_, err = adapter.SendText(ctx, ec, conversation.SendTextRequest{Body: text})
	return err
}
