package conversation_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/leadexit"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

var errLeadExitStepMissing = errors.New("whatsapp inbound: the lead exit step is not wired, the message cannot be checked for an exit")

func (uc *handleWhatsAppMessageUseCase) SetLeadExitStep(step leadexit.Step) {
	uc.exitStep = step
}

type whatsAppExitTarget struct {
	agentCtx        *agentContext
	leadRecord      *lead.Lead
	entryID         string
	entryType       shared.EntryType
	conversationID  string
	businessNumber  string
	receivedPhoneID string
	history         []*conversation.Message
}

func (t whatsAppExitTarget) workspaceID() string {
	if t.agentCtx == nil || t.agentCtx.wcCampaign == nil {
		return ""
	}
	return strings.TrimSpace(t.agentCtx.wcCampaign.WorkspaceID)
}

func (t whatsAppExitTarget) leadID() string {
	if t.leadRecord != nil {
		return t.leadRecord.ID
	}
	if t.agentCtx != nil && t.agentCtx.wcLeadRecord != nil {
		return t.agentCtx.wcLeadRecord.ID
	}
	return ""
}

func (t whatsAppExitTarget) campaignPhoneID() string {
	if t.agentCtx == nil || t.agentCtx.wcCampaign == nil {
		return ""
	}
	return t.agentCtx.wcCampaign.BusinessPhoneID
}

func (uc *handleWhatsAppMessageUseCase) runLeadExit(ctx context.Context, message *conversation.WhatsAppMessage, t whatsAppExitTarget) (leadexit.Result, error) {
	workspaceID := t.workspaceID()
	if message == nil || t.entryID == "" || workspaceID == "" {
		return leadexit.Result{}, nil
	}
	if uc.exitStep == nil {
		return leadexit.Result{}, errLeadExitStepMissing
	}
	replier := &whatsAppExitReplier{uc: uc, target: t, workspaceID: workspaceID, to: strings.TrimSpace(message.From)}
	in := whatsAppExitInbound(message, workspaceID, t)
	in.TriggerMessageID = uc.storedInboundID(message.ID)
	result, err := uc.exitStep.Run(ctx, in, replier)
	if err != nil {
		return result, fmt.Errorf("lead exit of %s: %w", message.ID, err)
	}
	return result, nil
}

func whatsAppExitInbound(message *conversation.WhatsAppMessage, workspaceID string, t whatsAppExitTarget) leadexit.Inbound {
	in := leadexit.Inbound{
		WorkspaceID:  workspaceID,
		LeadID:       t.leadID(),
		SenderNumber: strings.TrimSpace(message.From),
		EntryID:      t.entryID,
		EntryType:    string(t.entryType),
		Channel:      string(conversation.MessageChannelWhatsApp),
		MessageID:    strings.TrimSpace(message.ID),
		ReceivedAt:   parseWhatsAppTimestamp(message.Timestamp),
	}
	if message.Text != nil {
		in.Text = message.Text.Body
	}
	if message.Button != nil {
		in.Tap = &leadexit.Tap{Payload: message.Button.Payload, Text: message.Button.Text}
	}
	if message.Context != nil {
		in.ContextMessageID = strings.TrimSpace(message.Context.ID)
	}
	if last, ok := leadexit.LastTemplateIn(t.history); ok {
		in.LastTemplate = &last
	}
	return in
}

type whatsAppExitReplier struct {
	uc          *handleWhatsAppMessageUseCase
	target      whatsAppExitTarget
	workspaceID string
	to          string
}

func (r *whatsAppExitReplier) Reply(ctx context.Context, text string) error {
	if r.uc.serviceMessageBilling != nil {
		if err := r.uc.serviceMessageBilling.AllowSend(r.workspaceID); err != nil {
			return fmt.Errorf("the workspace cannot pay for the confirmation: %w", err)
		}
	}
	client, err := r.uc.resolveWhatsAppClient(r.target.campaignPhoneID(), r.target.receivedPhoneID)
	if err != nil {
		return err
	}
	sent, err := client.SendTextMessage(ctx, conversation.SendTextMessageInput{To: r.to, Body: text})
	if err != nil {
		return err
	}
	if r.uc.historyManager == nil {
		return errors.New("the confirmation was sent but there is no history to record it in")
	}
	record := conversation.MessageHistoryRecord{
		SentBy:         conversation.SentBySystem(),
		EntryID:        r.target.entryID,
		EntryType:      r.target.entryType,
		Channel:        conversation.MessageChannelWhatsApp,
		MessageType:    conversation.MessageTypeAIResponse,
		ConversationID: r.target.conversationID,
		From:           r.target.businessNumber,
		To:             r.to,
		Text:           text,
		Timestamp:      time.Now().UTC(),
	}
	if sent != nil {
		record.MessageID = strings.TrimSpace(sent.MessageID)
	}
	return r.uc.historyManager.Record(ctx, record)
}

func selectedOptionFromMessage(msg *conversation.WhatsAppMessage, tap *workflow.OptionSelection) *workflow.OptionSelection {
	if tap != nil {
		return tap
	}
	if msg == nil || msg.Interactive == nil {
		return nil
	}
	if br := msg.Interactive.ButtonReply; br != nil && strings.TrimSpace(br.ID) != "" {
		return &workflow.OptionSelection{ID: br.ID, Title: br.Title, Kind: "button"}
	}
	if lr := msg.Interactive.ListReply; lr != nil && strings.TrimSpace(lr.ID) != "" {
		return &workflow.OptionSelection{ID: lr.ID, Title: lr.Title, Kind: "list"}
	}
	return nil
}

func (uc *handleWhatsAppMessageUseCase) storedInboundID(wamid string) string {
	if uc.messageRepo == nil {
		return ""
	}
	return StoredWhatsAppMessageID(uc.messageRepo, wamid)
}
