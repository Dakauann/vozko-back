package whatsapp_outreach

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"vozko/domain/campaign"
	"vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/lead"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	wce "vozko/domain/whatsapp_campaign_entry"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/domain/workspace_template_access"
	"vozko/usecases/campaignguard"
)

type sendRules struct {
	deps     Deps
	cooldown *campaignguard.SpamGuard
}

type delivery struct {
	entry       *wce.WhatsAppCampaignEntry
	tmpl        *template.Template
	bodyParams  []string
	workspaceID string
	userID      string
	to          string
	leadID      string
	phoneID     string
	campaignID  string
}

func (r sendRules) sendablePhone(workspaceID, phoneID string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	phone, err := r.deps.Phones.FindByID(phoneID)
	if err != nil || phone == nil {
		return nil, wo.ErrBusinessPhoneNotFound
	}
	allowed, err := businessphone.CanWorkspaceSendFrom(workspaceID, phoneID, phone, r.deps.PhoneGrants)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, wo.ErrBusinessPhoneNotFound
	}
	if !phone.IsVerified() {
		return nil, wo.ErrPhoneNotConnected
	}
	return phone, nil
}

func (r sendRules) grantedTemplate(workspaceID, templateID string) (*template.Template, error) {
	return grantedTemplate(r.deps.Templates, r.deps.TemplateGrant, workspaceID, templateID)
}

func grantedTemplate(templates TemplateFinder, grant workspace_template_access.CheckAccessUseCase, workspaceID, templateID string) (*template.Template, error) {
	if templates == nil {
		return nil, wo.ErrTemplateNotFound
	}
	if grant == nil {
		return nil, wo.ErrTemplateForbidden
	}
	tmpl, err := templates.FindByID(templateID)
	if err != nil || tmpl == nil {
		return nil, wo.ErrTemplateNotFound
	}
	granted, err := grant.Execute(workspaceID, tmpl.ID)
	if err != nil {
		return nil, err
	}
	if !granted {
		return nil, wo.ErrTemplateForbidden
	}
	return tmpl, nil
}

func (r sendRules) refuseUnreachable(contact *lead.Lead) error {
	switch campaign.LeadRefusal(campaign.LeadFactsOf(contact)) {
	case "":
		return nil
	case campaign.SkipBlocked:
		return wo.ErrLeadBlocked
	case campaign.SkipOptedOut:
		return wo.ErrLeadOptedOut
	default:
		return wo.ErrInvalidPhone
	}
}

func (r sendRules) claimContact(ctx context.Context, workspaceID, leadID, phoneID string) (func(), error) {
	release, err := r.cooldown.Claim(ctx, workspaceID, leadID, phoneID)
	if errors.Is(err, campaignguard.ErrSendClaimed) {
		return nil, wo.ErrWithinSpamWindow
	}
	if err != nil {
		return nil, fmt.Errorf("whatsapp outreach: %w", err)
	}
	return release, nil
}

func keepsClaim(sendErr error) bool {
	return sendErr == nil || errors.Is(sendErr, wo.ErrSendOutcomeUnknown)
}

func (r sendRules) refuseIfSpam(ctx context.Context, workspaceID, leadID, phoneID string) error {
	cooling, err := r.cooldown.InCooldown(ctx, workspaceID, leadID, phoneID)
	if err != nil {
		return fmt.Errorf("whatsapp outreach: %w", err)
	}
	if cooling {
		return wo.ErrWithinSpamWindow
	}
	return nil
}

func (r sendRules) charge(ctx context.Context, d delivery, in template.BilledSendInput) (*template.BilledSendResult, error) {
	result, err := r.deps.Sender.Execute(ctx, in)
	if err == nil {
		return result, nil
	}
	r.markEntryFailed(d.entry.ID, result, err)
	if result != nil && result.Outcome == template.OutcomeUnknown {
		return nil, fmt.Errorf("%w: %w", wo.ErrSendOutcomeUnknown, err)
	}
	return nil, err
}

func (r sendRules) settle(ctx context.Context, d delivery, result *template.BilledSendResult) bool {
	if err := r.deps.Entries.UpdateStatus(d.entry.ID, wce.SendStatusSent, result.MessageID, 0, ""); err != nil {
		log.Printf("[whatsapp-outreach] could not mark entry %s as sent: %v", d.entry.ID, err)
	}
	r.storeTemplateInfo(d)
	recorded := r.recordMessage(ctx, d, result)

	if err := r.deps.CampaignSends.Record(d.leadID, d.phoneID, d.campaignID); err != nil {
		log.Printf("[whatsapp-outreach] could not record the send against lead %s: %v", d.leadID, err)
	}
	r.claimForSender(d)
	return recorded
}

func (r sendRules) claimForSender(d delivery) {
	if d.userID == "" {
		return
	}
	if _, err := r.deps.Assignments.ClaimIfUnassigned(d.entry.ID, string(shared.EntryTypeWhatsApp), d.phoneID, d.workspaceID, d.userID, ia.TriggerOutreachSent); err != nil {
		log.Printf("[whatsapp-outreach] sent from entry %s but could not assign it to %s: %v", d.entry.ID, d.userID, err)
	}
}

func (r sendRules) markEntryFailed(entryID string, result *template.BilledSendResult, sendErr error) {
	if result != nil && result.Outcome == template.OutcomeUnknown {
		return
	}
	message := sendErr.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	if err := r.deps.Entries.UpdateStatus(entryID, wce.SendStatusFailed, "", wce.FailureCode(sendErr), message); err != nil {
		log.Printf("[whatsapp-outreach] could not mark entry %s as failed: %v", entryID, err)
	}
}

func (r sendRules) storeTemplateInfo(d delivery) {
	meta := d.entry.Metadata
	if meta == nil {
		meta = map[string]interface{}{}
	}
	meta["template_info"] = d.tmpl.RenderInfo(d.bodyParams)
	if err := r.deps.Entries.UpdateMetadata(d.entry.ID, meta); err != nil {
		log.Printf("[whatsapp-outreach] could not store template info on entry %s: %v", d.entry.ID, err)
	}
}

func (r sendRules) recordMessage(ctx context.Context, d delivery, result *template.BilledSendResult) bool {
	info := d.tmpl.RenderInfo(d.bodyParams)
	bodyText, _ := info["body_text"].(string)
	metaBytes, err := json.Marshal(info)
	if err != nil {
		log.Printf("[whatsapp-outreach] could not marshal template metadata for entry %s: %v", d.entry.ID, err)
		return false
	}

	record := conversation.MessageHistoryRecord{
		SentBy:      conversation.SentByPerson(d.userID),
		EntryID:     d.entry.ID,
		EntryType:   shared.EntryTypeWhatsApp,
		Channel:     conversation.MessageChannelWhatsApp,
		MessageType: conversation.MessageTypeTemplate,
		MessageID:   result.MessageID,
		From:        d.userID,
		To:          d.to,
		Text:        bodyText,
		Timestamp:   r.deps.Now(),
		Metadata:    json.RawMessage(metaBytes),
	}
	if err := r.deps.History.Record(ctx, record); err != nil {
		log.Printf("[whatsapp-outreach] template delivered but not recorded on entry %s: %v", d.entry.ID, err)
		return false
	}
	return true
}

func renderedPreview(tmpl *template.Template, bodyParams []string) string {
	text, _ := tmpl.RenderInfo(bodyParams)["body_text"].(string)
	return text
}
