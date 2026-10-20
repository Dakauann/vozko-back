package conversation_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/lead"
	"vozko/domain/shared"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

var (
	errContinuationUnwired       = errors.New("whatsapp continuation: repositories are not wired")
	errContinuationAgentChanged  = errors.New("whatsapp continuation: the conversation is no longer answered by the agent that started the run")
	errContinuationNoBusinessNum = errors.New("whatsapp continuation: the conversation has no business number to reply from")
)

type whatsAppContinuation struct {
	entry    *wce.WhatsAppCampaignEntry
	campaign *wc.Campaign
	lead     *lead.Lead
}

func (c whatsAppContinuation) windowPhoneID() string {
	if id := strings.TrimSpace(c.entry.ReceivedBusinessPhoneID); id != "" {
		return id
	}
	return strings.TrimSpace(c.campaign.BusinessPhoneID)
}

func (c whatsAppContinuation) outboundPhoneID() string {
	if id := strings.TrimSpace(c.campaign.BusinessPhoneID); id != "" {
		return id
	}
	return strings.TrimSpace(c.entry.ReceivedBusinessPhoneID)
}

func (uc *handleWhatsAppMessageUseCase) continuationTarget(turn AgentContinuationTurn) (whatsAppContinuation, error) {
	if uc.wcEntryRepo == nil || uc.wcCampaignRepo == nil || uc.leadRepo == nil || uc.messageRepo == nil {
		return whatsAppContinuation{}, errContinuationUnwired
	}
	entry, err := uc.wcEntryRepo.FindByID(turn.EntryID)
	if err != nil || entry == nil {
		return whatsAppContinuation{}, continuationReadFailure(fmt.Sprintf("entry %s", turn.EntryID), err, wce.ErrEntryNotFound)
	}
	campaign, err := uc.wcCampaignRepo.FindByID(entry.CampaignID)
	if err != nil || campaign == nil {
		return whatsAppContinuation{}, continuationReadFailure(fmt.Sprintf("campaign %s", entry.CampaignID), err, wc.ErrCampaignNotFound)
	}
	leadRecord, err := uc.leadRepo.FindByID(campaign.WorkspaceID, entry.LeadID)
	if err != nil || leadRecord == nil {
		return whatsAppContinuation{}, continuationReadFailure(fmt.Sprintf("lead %s", entry.LeadID), err, lead.ErrLeadNotFound)
	}
	return whatsAppContinuation{entry: entry, campaign: campaign, lead: leadRecord}, nil
}

func continuationReadFailure(what string, readErr, missing error) error {
	if readErr != nil {
		return turnNotStarted(fmt.Errorf("whatsapp continuation: %s: %w", what, readErr))
	}
	return fmt.Errorf("whatsapp continuation: %s: %w", what, missing)
}

func (uc *handleWhatsAppMessageUseCase) ContinuationWindowOpen(_ context.Context, turn AgentContinuationTurn) (bool, error) {
	target, err := uc.continuationTarget(turn)
	if err != nil {
		return false, err
	}
	if uc.messageWindowRepo == nil {
		return false, errContinuationUnwired
	}
	return uc.messageWindowRepo.IsWindowOpen(target.lead.ID, target.windowPhoneID())
}

func (uc *handleWhatsAppMessageUseCase) ContinueAgentTurn(ctx context.Context, turn AgentContinuationTurn) error {
	target, err := uc.continuationTarget(turn)
	if err != nil {
		return err
	}
	if err := refusedContact(target.lead); err != nil {
		return err
	}
	actx := &agentContext{wcCampaign: target.campaign, wcEntry: target.entry, wcLeadRecord: target.lead}
	responsesEnabled := target.campaign.EnableAgentResponses
	if target.entry.AutomationEnabled != nil {
		responsesEnabled = *target.entry.AutomationEnabled
	}
	if !uc.prepareAutomation(actx, responsesEnabled) || actx.agent == nil || actx.agent.ID != turn.AgentID {
		return errContinuationAgentChanged
	}

	businessNumber, err := uc.continuationBusinessNumber(target)
	if err != nil {
		return err
	}

	return uc.sendAgentReply(withAgentTurn(ctx, actx), whatsAppAgentReply{
		agentCtx: actx,
		whatsappCtx: WhatsAppContext{
			UserPhoneNumber:     target.lead.Number,
			UserName:            target.lead.RealName(),
			ConversationID:      target.lead.ID,
			MessageReceivedTime: time.Now().UTC(),
			CampaignName:        target.campaign.Name,
			AgentName:           actx.agent.Name,
			Metadata:            target.entry.Metadata,
		},
		entryID:         target.entry.ID,
		entryType:       shared.EntryTypeWhatsApp,
		conversationID:  target.lead.ID,
		recipient:       target.lead.Number,
		businessNumber:  businessNumber,
		outboundPhoneID: target.outboundPhoneID(),
		receivedPhoneID: target.entry.ReceivedBusinessPhoneID,
		triggerNote:     turn.Note,
	})
}

func (uc *handleWhatsAppMessageUseCase) continuationBusinessNumber(target whatsAppContinuation) (string, error) {
	if uc.businessPhoneRepo == nil {
		return "", errContinuationUnwired
	}
	phone, err := uc.businessPhoneRepo.FindByID(target.windowPhoneID())
	if err != nil || phone == nil || strings.TrimSpace(phone.DisplayPhoneNumber) == "" {
		return "", errors.Join(errContinuationNoBusinessNum, err)
	}
	return strings.TrimSpace(phone.DisplayPhoneNumber), nil
}

var _ AgentTurnContinuer = (*handleWhatsAppMessageUseCase)(nil)
