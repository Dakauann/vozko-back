package container

import (
	conversation_domain "vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/whatsapp_campaign"
)

type entryPhones struct {
	resolver  conversation_domain.CampaignWorkspaceResolver
	campaigns whatsapp_campaign.Repository
}

func (p entryPhones) BusinessPhoneForEntry(entryID, entryType string) string {
	if shared.EntryType(entryType) != shared.EntryTypeWhatsApp || p.resolver == nil || p.campaigns == nil {
		return ""
	}
	campaignID, _ := p.resolver.GetEntryCampaignID(entryID, entryType)
	if campaignID == "" {
		return ""
	}
	campaign, err := p.campaigns.FindByID(campaignID)
	if err != nil || campaign == nil {
		return ""
	}
	return campaign.BusinessPhoneID
}
