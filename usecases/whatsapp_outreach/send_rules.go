package whatsapp_outreach

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"vozko/domain/conversation"
	lcs "vozko/domain/lead_campaign_send"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	wce "vozko/domain/whatsapp_campaign_entry"
	wo "vozko/domain/whatsapp_outreach"
)

type sendRules struct {
	deps Deps
}

type delivery struct {
	entry      *wce.WhatsAppCampaignEntry
	tmpl       *template.Template
	bodyParams []string
	userID     string
	to         string
	leadID     string
	phoneID    string
	campaignID string
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
	tmpl, err := r.deps.Templates.FindByID(templateID)
	if err != nil || tmpl == nil {
		return nil, wo.ErrTemplateNotFound
	}
	granted, err := r.deps.TemplateGrant.Execute(workspaceID, tmpl.ID)
	if err != nil {
		return nil, err
	}
	if !granted {
		return nil, wo.ErrTemplateForbidden
	}
	return tmpl, nil
}

func (r sendRules) refuseIfSpam(ctx context.Context, workspaceID, leadID, phoneID string) error {
	days, err := r.deps.SpamPolicy.SpamProtectionDays(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("whatsapp outreach: could not read the spam protection policy: %w", err)
	}
	if days <= 0 {
		return nil
	}
	lastSent, err := r.deps.CampaignSends.GetLastSendTime(leadID, phoneID)
	if err != nil {
		return fmt.Errorf("whatsapp outreach: could not read the last send to this contact: %w", err)
	}
	if lcs.WithinSpamWindow(lastSent, days, r.deps.Now()) {
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
	return recorded
}

func (r sendRules) markEntryFailed(entryID string, result *template.BilledSendResult, sendErr error) {
	if result != nil && result.Outcome == template.OutcomeUnknown {
		return
	}
	message := sendErr.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	if err := r.deps.Entries.UpdateStatus(entryID, wce.SendStatusFailed, "", 0, message); err != nil {
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
