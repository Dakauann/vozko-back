package whatsapp_campaign_usecase

import (
	"vozko/domain/campaign"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/usecases/campaignautomation"
)

func guardCampaignChange(c *wc.Campaign, automation AutomationCheck, changesWhatIsSent bool, entries []campaign.EntryMetadata) error {
	if err := campaign.RefuseSelectionChange(c.Source, changesWhatIsSent); err != nil {
		return err
	}
	return campaignautomation.Require(automation, c.WorkspaceID, c.Automation(), entries)
}

func entriesMetadata(inputs []wc.EntryInput) []campaign.EntryMetadata {
	entries := make([]campaign.EntryMetadata, 0, len(inputs))
	for _, in := range inputs {
		entries = append(entries, campaign.EntryMetadataOf("", in.Number, in.Metadata))
	}
	return entries
}
