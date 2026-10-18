package leadexit_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/buttonaction"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/leadexit"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type CampaignEntries interface {
	FindByID(id string) (*wce.WhatsAppCampaignEntry, error)
	FindByMessageID(messageID string) (*wce.WhatsAppCampaignEntry, error)
}

type Campaigns interface {
	FindByID(campaignID string) (*wc.Campaign, error)
}

type StoredMessages interface {
	GetByWhatsAppMessageID(wamid string) (*conversation.Message, error)
}

type LeadDirectory interface {
	FindByID(workspaceID, id string) (*lead.Lead, error)
}

type TemplateDirectory interface {
	FindByID(templateID string) (*template.Template, error)
}

type SentTemplatesDeps struct {
	Entries   CampaignEntries
	Campaigns Campaigns
	Messages  StoredMessages
	Leads     LeadDirectory
	Templates TemplateDirectory
}

type SentTemplates struct {
	deps SentTemplatesDeps
}

var _ SentMessages = (*SentTemplates)(nil)

func NewSentTemplates(deps SentTemplatesDeps) (*SentTemplates, error) {
	if deps.Entries == nil || deps.Campaigns == nil || deps.Messages == nil || deps.Leads == nil || deps.Templates == nil {
		return nil, leadexit.ErrUnavailable
	}
	return &SentTemplates{deps: deps}, nil
}

func (r *SentTemplates) SentTemplate(_ context.Context, messageID string) (*leadexit.SentTemplate, error) {
	messageID = strings.TrimSpace(messageID)
	stored, err := r.deps.Messages.GetByWhatsAppMessageID(messageID)
	if err != nil && !errors.Is(err, conversation.ErrMessageNotFound) {
		return nil, fmt.Errorf("message %s: %w", messageID, err)
	}
	if stored != nil {
		return r.fromStoredMessage(messageID, stored)
	}
	return r.fromCampaignEntry(messageID)
}

func (r *SentTemplates) fromStoredMessage(messageID string, stored *conversation.Message) (*leadexit.SentTemplate, error) {
	sent, ok := leadexit.TemplateOfMessage(stored)
	if !ok {
		return nil, nil
	}
	sent.MessageID = messageID
	campaign, err := r.campaignOfEntry(stored.EntryID)
	if err != nil || campaign == nil {
		return nil, err
	}
	sent.WorkspaceID = campaign.WorkspaceID
	if sent.CampaignID != campaign.ID {
		return &sent, nil
	}
	current, err := r.stillSends(campaign, sent.Template)
	if err != nil {
		return nil, err
	}
	if current {
		sent.TemplateID = campaign.TemplateID
	}
	return &sent, nil
}

func (r *SentTemplates) stillSends(campaign *wc.Campaign, sent buttonaction.TemplateRef) (bool, error) {
	if strings.TrimSpace(campaign.TemplateID) == "" {
		return false, nil
	}
	current, err := r.deps.Templates.FindByID(campaign.TemplateID)
	if errors.Is(err, template.ErrTemplateNotFound) || (err == nil && current == nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("template %s of campaign %s: %w", campaign.TemplateID, campaign.ID, err)
	}
	return buttonaction.RefOf(current) == sent, nil
}

func (r *SentTemplates) fromCampaignEntry(messageID string) (*leadexit.SentTemplate, error) {
	entry, err := r.deps.Entries.FindByMessageID(messageID)
	if errors.Is(err, wce.ErrEntryNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("campaign entry of %s: %w", messageID, err)
	}
	campaign, err := r.campaign(entry.CampaignID)
	if err != nil || campaign == nil {
		return nil, err
	}
	recipient, err := r.deps.Leads.FindByID(campaign.WorkspaceID, entry.LeadID)
	if errors.Is(err, lead.ErrLeadNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lead of entry %s: %w", entry.ID, err)
	}
	return &leadexit.SentTemplate{
		MessageID: messageID, WorkspaceID: campaign.WorkspaceID, RecipientNumber: recipient.Number,
		TemplateID: campaign.TemplateID, CampaignID: campaign.ID,
	}, nil
}

func (r *SentTemplates) campaignOfEntry(entryID string) (*wc.Campaign, error) {
	entry, err := r.deps.Entries.FindByID(entryID)
	if errors.Is(err, wce.ErrEntryNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("entry %s: %w", entryID, err)
	}
	return r.campaign(entry.CampaignID)
}

func (r *SentTemplates) campaign(campaignID string) (*wc.Campaign, error) {
	campaign, err := r.deps.Campaigns.FindByID(campaignID)
	if errors.Is(err, wc.ErrCampaignNotFound) || (err == nil && campaign == nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("campaign %s: %w", campaignID, err)
	}
	return campaign, nil
}
