package whatsapp_outreach

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
)

type SpamPolicyReader interface {
	SpamProtectionDays(ctx context.Context, workspaceID string) (int, error)
}

type WindowReader interface {
	IsWindowOpen(leadID, businessPhoneID string) (bool, error)
}

type RateLimiter interface {
	Allow(ctx context.Context, workspaceID string, limit int, window time.Duration) (bool, error)
}

type Deps struct {
	Phones        businessphone.Repository
	PhoneGrants   workspace_phone_access.Repository
	Templates     template.Repository
	TemplateGrant workspace_template_access.CheckAccessUseCase

	Leads         lead.Repository
	Entries       wce.Repository
	Campaigns     wc.Repository
	EnsureOrganic wc.EnsureOrganicCoexistenceCampaignUseCase
	Windows       WindowReader
	CampaignSends lcs.Repository
	SpamPolicy    SpamPolicyReader
	History       conversation.MessageHistoryManager
	Sender        template.BilledTemplateSendUseCase
	Limiter       RateLimiter
	HourlySendCap int
	Now           func() time.Time
}

type startConversationUseCase struct {
	deps Deps
}

func NewStartConversationUseCase(deps Deps) (wo.StartOfficialConversationUseCase, error) {
	var missing []string
	if deps.Phones == nil {
		missing = append(missing, "business phone repository")
	}
	if deps.Templates == nil {
		missing = append(missing, "template repository")
	}
	if deps.Leads == nil {
		missing = append(missing, "lead repository")
	}
	if deps.Entries == nil {
		missing = append(missing, "campaign entry repository")
	}
	if deps.EnsureOrganic == nil {
		missing = append(missing, "organic campaign use case")
	}
	if deps.Sender == nil {
		missing = append(missing, "billed template sender")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("whatsapp outreach: %s not configured", strings.Join(missing, ", "))
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	return &startConversationUseCase{deps: deps}, nil
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

	phone, err := uc.deps.Phones.FindByID(in.BusinessPhoneID)
	if err != nil || phone == nil {
		return nil, wo.ErrBusinessPhoneNotFound
	}
	allowed, err := businessphone.CanWorkspaceSendFrom(in.WorkspaceID, in.BusinessPhoneID, phone, uc.deps.PhoneGrants)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, wo.ErrBusinessPhoneNotFound
	}
	if !phone.IsVerified() {
		return nil, wo.ErrPhoneNotConnected
	}

	tmpl, err := uc.deps.Templates.FindByID(in.TemplateID)
	if err != nil || tmpl == nil {
		return nil, wo.ErrTemplateNotFound
	}
	if uc.deps.TemplateGrant != nil {
		granted, accessErr := uc.deps.TemplateGrant.Execute(in.WorkspaceID, tmpl.ID)
		if accessErr != nil {
			return nil, accessErr
		}
		if !granted {
			return nil, wo.ErrTemplateForbidden
		}
	}

	campaign, _, err := uc.deps.EnsureOrganic.Execute(in.WorkspaceID, phone.ID, phone.DisplayPhoneNumber)
	if err != nil || campaign == nil {
		return nil, fmt.Errorf("whatsapp outreach: could not resolve the conversation container: %w", err)
	}
	if !uc.departmentAllows(in, campaign) {
		return nil, wo.ErrDepartmentForbidden
	}

	leadRecord, _, err := uc.deps.Leads.FindOrCreate(in.WorkspaceID, number, lead.LeadUpdate{Name: strings.TrimSpace(in.Name)})
	if err != nil {
		return nil, err
	}
	if leadRecord.Blocked {
		return nil, wo.ErrLeadBlocked
	}

	entry, entryExisted := uc.findExistingEntry(number, phone.ID)

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
	if err := uc.refuseIfTooFast(ctx, in.WorkspaceID); err != nil {
		return nil, err
	}

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

	sendResult, sendErr := uc.deps.Sender.Execute(ctx, template.BilledSendInput{
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
	if sendErr != nil {
		uc.markEntryFailed(entry.ID, sendResult, sendErr)
		return nil, sendErr
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

	if err := uc.deps.Entries.UpdateStatus(entry.ID, wce.SendStatusSent, sendResult.MessageID, 0, ""); err != nil {
		log.Printf("[whatsapp-outreach] could not mark entry %s as sent: %v", entry.ID, err)
	}
	uc.storeTemplateInfo(entry, tmpl, in.BodyParams)
	result.Recorded = uc.recordMessage(ctx, entry, tmpl, in, leadRecord, sendResult)

	if uc.deps.CampaignSends != nil {
		if err := uc.deps.CampaignSends.Record(leadRecord.ID, phone.ID, campaign.ID); err != nil {
			log.Printf("[whatsapp-outreach] could not record the send against lead %s: %v", leadRecord.ID, err)
		}
	}

	return result, nil
}

func (uc *startConversationUseCase) findExistingEntry(number, phoneID string) (*wce.WhatsAppCampaignEntry, bool) {
	existing, err := uc.deps.Entries.FindByNumberAndBusinessPhone(number, phoneID)
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

func (uc *startConversationUseCase) refuseIfSpam(ctx context.Context, workspaceID, leadID, phoneID string) error {
	if uc.deps.SpamPolicy == nil || uc.deps.CampaignSends == nil {
		return nil
	}
	days, err := uc.deps.SpamPolicy.SpamProtectionDays(ctx, workspaceID)
	if err != nil || days <= 0 {
		return nil
	}
	lastSent, err := uc.deps.CampaignSends.GetLastSendTime(leadID, phoneID)
	if err != nil {
		return nil
	}
	if lcs.WithinSpamWindow(lastSent, days, uc.deps.Now()) {
		return wo.ErrWithinSpamWindow
	}
	return nil
}

func (uc *startConversationUseCase) refuseIfTooFast(ctx context.Context, workspaceID string) error {
	if uc.deps.Limiter == nil || uc.deps.HourlySendCap <= 0 {
		return nil
	}
	ok, err := uc.deps.Limiter.Allow(ctx, workspaceID, uc.deps.HourlySendCap, time.Hour)
	if err != nil {
		log.Printf("[whatsapp-outreach] rate limiter unavailable for workspace %s: %v", workspaceID, err)
		return nil
	}
	if !ok {
		return wo.ErrRateLimited
	}
	return nil
}

func (uc *startConversationUseCase) markEntryFailed(entryID string, result *template.BilledSendResult, sendErr error) {
	code, message := 0, sendErr.Error()
	if result != nil && result.Outcome == template.OutcomeUnknown {
		return
	}
	if len(message) > 500 {
		message = message[:500]
	}
	if err := uc.deps.Entries.UpdateStatus(entryID, wce.SendStatusFailed, "", code, message); err != nil {
		log.Printf("[whatsapp-outreach] could not mark entry %s as failed: %v", entryID, err)
	}
}

func (uc *startConversationUseCase) storeTemplateInfo(entry *wce.WhatsAppCampaignEntry, tmpl *template.Template, params []string) {
	meta := entry.Metadata
	if meta == nil {
		meta = map[string]interface{}{}
	}
	meta["template_info"] = tmpl.RenderInfo(params)
	if err := uc.deps.Entries.UpdateMetadata(entry.ID, meta); err != nil {
		log.Printf("[whatsapp-outreach] could not store template info on entry %s: %v", entry.ID, err)
	}
}

func (uc *startConversationUseCase) recordMessage(
	ctx context.Context,
	entry *wce.WhatsAppCampaignEntry,
	tmpl *template.Template,
	in wo.StartConversationInput,
	leadRecord *lead.Lead,
	sendResult *template.BilledSendResult,
) bool {
	if uc.deps.History == nil {
		return false
	}

	info := tmpl.RenderInfo(in.BodyParams)
	bodyText, _ := info["body_text"].(string)
	metaBytes, err := json.Marshal(info)
	if err != nil {
		log.Printf("[whatsapp-outreach] could not marshal template metadata for entry %s: %v", entry.ID, err)
		return false
	}

	record := conversation.MessageHistoryRecord{
		EntryID:     entry.ID,
		EntryType:   shared.EntryTypeWhatsApp,
		Channel:     conversation.MessageChannelWhatsApp,
		MessageType: conversation.MessageTypeTemplate,
		MessageID:   sendResult.MessageID,
		From:        in.UserID,
		To:          leadRecord.Number,
		Text:        bodyText,
		Timestamp:   uc.deps.Now(),
		Metadata:    json.RawMessage(metaBytes),
	}
	if err := uc.deps.History.Record(ctx, conversation.MessageDirectionOutbound, record); err != nil {
		log.Printf("[whatsapp-outreach] template delivered but not recorded on entry %s: %v", entry.ID, err)
		return false
	}
	return true
}
