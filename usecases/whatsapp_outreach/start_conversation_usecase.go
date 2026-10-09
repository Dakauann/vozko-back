package whatsapp_outreach

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	lcs "vozko/domain/lead_campaign_send"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/domain/workspace_phone_access"
	"vozko/domain/workspace_template_access"
	"vozko/usecases/campaignguard"
)

type SpamPolicyReader = campaignguard.SpamPolicy

type WindowReader interface {
	IsWindowOpen(leadID, businessPhoneID string) (bool, error)
}

type ConversationClaimer interface {
	ClaimIfUnassigned(entryID, entryType, businessPhoneID, workspaceID, userID, trigger string) (bool, error)
}

type Deps struct {
	Phones        businessphone.Repository
	PhoneGrants   workspace_phone_access.Repository
	Templates     template.Repository
	TemplateGrant workspace_template_access.CheckAccessUseCase

	Leads           lead.Repository
	Entries         wce.Repository
	Campaigns       wc.Repository
	EnsureReceptive wc.EnsureReceptiveContainerUseCase
	Windows         WindowReader
	CampaignSends   lcs.Repository
	SpamPolicy      SpamPolicyReader
	SendClaims      campaignguard.SendClaims
	History         conversation.MessageHistoryManager
	Assignments     ConversationClaimer
	Sender          template.BilledTemplateSendUseCase
	Now             func() time.Time
}

type startConversationUseCase struct {
	sendRules
}

func NewStartConversationUseCase(deps Deps) (wo.StartOfficialConversationUseCase, error) {
	rules, err := newSendRules(deps, map[string]bool{
		"organic campaign use case": deps.EnsureReceptive != nil,
	})
	if err != nil {
		return nil, err
	}
	return &startConversationUseCase{sendRules: rules}, nil
}

func (uc *startConversationUseCase) Execute(ctx context.Context, in wo.StartConversationInput) (*wo.StartedConversation, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, template.ErrWorkspaceRequired
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return nil, template.ErrIdempotencyKeyRequired
	}

	number := lead.NormalizeNumber(in.PhoneNumber)
	if number == "" {
		return nil, wo.ErrInvalidPhone
	}

	phone, err := uc.sendablePhone(in.WorkspaceID, in.BusinessPhoneID)
	if err != nil {
		return nil, err
	}

	tmpl, err := uc.grantedTemplate(in.WorkspaceID, in.TemplateID)
	if err != nil {
		return nil, err
	}

	campaign, _, err := uc.deps.EnsureReceptive.Execute(in.WorkspaceID, phone.ID, phone.DisplayPhoneNumber)
	if err != nil || campaign == nil {
		return nil, fmt.Errorf("whatsapp outreach: could not resolve the conversation container: %w", err)
	}
	if !uc.departmentAllows(in, campaign) {
		return nil, wo.ErrDepartmentForbidden
	}

	leadRecord, _, err := uc.deps.Leads.FindOrCreate(in.WorkspaceID, number, lead.LeadUpdate{Source: lead.SourceImport, Name: strings.TrimSpace(in.Name)})
	if err != nil {
		return nil, err
	}
	if err := uc.refuseUnreachable(leadRecord); err != nil {
		return nil, err
	}

	entry, entryExisted := uc.findExistingEntry(in.WorkspaceID, number, phone.ID)

	if uc.deps.Windows != nil && !tmpl.IsAuthentication() {
		open, windowErr := uc.deps.Windows.IsWindowOpen(leadRecord.ID, phone.ID)
		if windowErr != nil {
			return nil, fmt.Errorf("whatsapp outreach: could not read the messaging window: %w", windowErr)
		}
		if open {
			result := &wo.StartedConversation{
				LeadID:              leadRecord.ID,
				EntryType:           string(shared.EntryTypeWhatsApp),
				ConversationExisted: entryExisted,
			}
			if entry != nil {
				result.EntryID = entry.ID
			}
			return result, wo.ErrWindowAlreadyOpen
		}
	}

	if err := uc.refuseIfSpam(ctx, in.WorkspaceID, leadRecord.ID, phone.ID); err != nil {
		return nil, err
	}
	release, err := uc.claimContact(ctx, in.WorkspaceID, leadRecord.ID, phone.ID)
	if err != nil {
		return nil, err
	}
	sent := false
	defer func() {
		if !sent {
			release()
		}
	}()

	if entry == nil {
		entry = &wce.WhatsAppCampaignEntry{
			ID:         uuid.New().String(),
			CampaignID: campaign.ID,
			LeadID:     leadRecord.ID,
			Status:     wce.SendStatusPending,
			Variables:  in.BodyParams,
		}
		if createErr := uc.deps.Entries.Create(entry); createErr != nil {
			if errors.Is(createErr, wce.ErrEntryDuplicate) {
				if existing, findErr := uc.deps.Entries.FindByCampaignAndLead(campaign.ID, leadRecord.ID); findErr == nil && existing != nil {
					entry, entryExisted = existing, true
				} else {
					return nil, createErr
				}
			} else {
				return nil, createErr
			}
		}
	}

	d := delivery{
		entry:       entry,
		tmpl:        tmpl,
		bodyParams:  in.BodyParams,
		workspaceID: in.WorkspaceID,
		userID:      in.UserID,
		to:          leadRecord.Number,
		leadID:      leadRecord.ID,
		phoneID:     phone.ID,
		campaignID:  campaign.ID,
	}
	sendResult, err := uc.charge(ctx, d, template.BilledSendInput{
		WorkspaceID:     in.WorkspaceID,
		UserID:          in.UserID,
		IdempotencyKey:  in.IdempotencyKey,
		BusinessPhoneID: phone.ID,
		TemplateID:      tmpl.ID,
		ToNumber:        lead.NormalizeWhatsAppNumber(number),
		BodyParams:      in.BodyParams,
		HeaderParams:    in.HeaderParams,
		CampaignID:      campaign.ID,
		EntryID:         entry.ID,
	})
	sent = keepsClaim(err)
	if err != nil {
		return nil, err
	}

	result := &wo.StartedConversation{
		EntryID:             entry.ID,
		EntryType:           string(shared.EntryTypeWhatsApp),
		LeadID:              leadRecord.ID,
		AttemptID:           sendResult.AttemptID,
		MessageID:           sendResult.MessageID,
		ConversationExisted: entryExisted,
		Replayed:            sendResult.Replayed,
		ChargedMicros:       sendResult.ChargedMicros,
	}
	if sendResult.Replayed {
		result.Recorded = true
		return result, nil
	}

	result.Recorded = uc.settle(ctx, d, sendResult)
	return result, nil
}

func (uc *startConversationUseCase) findExistingEntry(workspaceID, number, phoneID string) (*wce.WhatsAppCampaignEntry, bool) {
	existing, err := uc.deps.Entries.FindByNumberBusinessPhoneAndWorkspace(number, phoneID, workspaceID)
	if err != nil || existing == nil {
		return nil, false
	}
	return existing, true
}

func (uc *startConversationUseCase) departmentAllows(in wo.StartConversationInput, campaign *wc.Campaign) bool {
	if in.IsAdmin || len(in.DepartmentIDs) == 0 {
		return true
	}
	if strings.TrimSpace(campaign.DepartmentID) == "" {
		return true
	}
	for _, id := range in.DepartmentIDs {
		if id == campaign.DepartmentID {
			return true
		}
	}
	return false
}
