package whatsapp_outreach

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"vozko/domain/lead"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	wo "vozko/domain/whatsapp_outreach"
)

type conversationTemplateUseCase struct {
	sendRules
}

type conversationTarget struct {
	entry    *wce.WhatsAppCampaignEntry
	campaign *wc.Campaign
	phone    *businessphone.WhatsAppBusinessPhoneNumber
	contact  *lead.Lead
	tmpl     *template.Template
}

func NewConversationTemplateUseCase(deps Deps) (wo.ConversationTemplateUseCase, error) {
	rules, err := newSendRules(deps, map[string]bool{
		"campaign repository": deps.Campaigns != nil,
	})
	if err != nil {
		return nil, err
	}
	return &conversationTemplateUseCase{sendRules: rules}, nil
}

func (uc *conversationTemplateUseCase) Check(ctx context.Context, in wo.ConversationTemplateInput) (*wo.ConversationTemplate, error) {
	target, err := uc.resolve(ctx, in)
	if err != nil {
		return nil, err
	}
	return &wo.ConversationTemplate{
		Name:    target.tmpl.Name,
		Preview: renderedPreview(target.tmpl, in.BodyParams),
	}, nil
}

func (uc *conversationTemplateUseCase) Send(ctx context.Context, in wo.ConversationTemplateInput) (*wo.SentConversationTemplate, error) {
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return nil, template.ErrIdempotencyKeyRequired
	}
	target, err := uc.resolve(ctx, in)
	if err != nil {
		return nil, err
	}

	d := delivery{
		entry:      target.entry,
		tmpl:       target.tmpl,
		bodyParams: in.BodyParams,
		userID:     in.UserID,
		to:         target.contact.Number,
		leadID:     target.contact.ID,
		phoneID:    target.phone.ID,
		campaignID: target.campaign.ID,
	}
	result, err := uc.charge(ctx, d, template.BilledSendInput{
		WorkspaceID:     in.WorkspaceID,
		UserID:          in.UserID,
		IdempotencyKey:  in.IdempotencyKey,
		BusinessPhoneID: target.phone.ID,
		TemplateID:      target.tmpl.ID,
		ToNumber:        lead.NormalizeWhatsAppNumber(target.contact.Number),
		BodyParams:      in.BodyParams,
		HeaderParams:    in.HeaderParams,
		CampaignID:      target.campaign.ID,
		EntryID:         target.entry.ID,
	})
	if err != nil {
		return nil, err
	}

	sent := &wo.SentConversationTemplate{
		AttemptID:     result.AttemptID,
		MessageID:     result.MessageID,
		ChargedMicros: result.ChargedMicros,
		Replayed:      result.Replayed,
		Recorded:      result.Replayed,
	}
	if !result.Replayed {
		sent.Recorded = uc.settle(ctx, d, result)
	}
	return sent, nil
}

func (uc *conversationTemplateUseCase) resolve(ctx context.Context, in wo.ConversationTemplateInput) (*conversationTarget, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, template.ErrWorkspaceRequired
	}

	entry, campaign, err := uc.conversation(in.WorkspaceID, in.EntryID)
	if err != nil {
		return nil, err
	}

	phone, err := uc.sendablePhone(in.WorkspaceID, campaign.BusinessPhoneID)
	if err != nil {
		return nil, err
	}

	tmpl, err := uc.grantedTemplate(in.WorkspaceID, in.TemplateID)
	if err != nil {
		return nil, err
	}
	if err := tmpl.EnsureSendable(); err != nil {
		return nil, err
	}
	if !tmpl.BelongsToWABA(phone.WABAId) {
		return nil, template.ErrTemplatePhoneMismatch
	}
	if err := tmpl.ValidateParams(in.BodyParams, in.HeaderParams); err != nil {
		return nil, err
	}

	contact, err := uc.contact(in.WorkspaceID, entry.LeadID)
	if err != nil {
		return nil, err
	}
	if contact.Blocked {
		return nil, wo.ErrLeadBlocked
	}

	if err := uc.refuseIfSpam(ctx, in.WorkspaceID, contact.ID, phone.ID); err != nil {
		return nil, err
	}

	return &conversationTarget{entry: entry, campaign: campaign, phone: phone, contact: contact, tmpl: tmpl}, nil
}

func (uc *conversationTemplateUseCase) conversation(workspaceID, entryID string) (*wce.WhatsAppCampaignEntry, *wc.Campaign, error) {
	entry, err := uc.deps.Entries.FindByID(entryID)
	if errors.Is(err, wce.ErrEntryNotFound) || (err == nil && entry == nil) {
		return nil, nil, wo.ErrConversationNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("whatsapp outreach: could not load the conversation: %w", err)
	}

	campaign, err := uc.deps.Campaigns.FindByID(entry.CampaignID)
	if errors.Is(err, wc.ErrCampaignNotFound) || (err == nil && campaign == nil) {
		return nil, nil, wo.ErrConversationNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("whatsapp outreach: could not load the conversation's number: %w", err)
	}
	if campaign.WorkspaceID != workspaceID {
		return nil, nil, wo.ErrConversationNotFound
	}
	return entry, campaign, nil
}

func (uc *conversationTemplateUseCase) contact(workspaceID, leadID string) (*lead.Lead, error) {
	contact, err := uc.deps.Leads.FindByID(workspaceID, leadID)
	if errors.Is(err, lead.ErrLeadNotFound) || (err == nil && contact == nil) {
		return nil, wo.ErrConversationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("whatsapp outreach: could not load the contact: %w", err)
	}
	return contact, nil
}

func newSendRules(deps Deps, specific map[string]bool) (sendRules, error) {
	required := map[string]bool{
		"business phone repository": deps.Phones != nil,
		"template repository":       deps.Templates != nil,
		"template grant check":      deps.TemplateGrant != nil,
		"lead repository":           deps.Leads != nil,
		"campaign entry repository": deps.Entries != nil,
		"campaign send history":     deps.CampaignSends != nil,
		"spam protection policy":    deps.SpamPolicy != nil,
		"message history":           deps.History != nil,
		"billed template sender":    deps.Sender != nil,
	}
	for name, present := range specific {
		required[name] = present
	}

	var missing []string
	for name, present := range required {
		if !present {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return sendRules{}, fmt.Errorf("whatsapp outreach: %s not configured", strings.Join(missing, ", "))
	}

	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	return sendRules{deps: deps}, nil
}
