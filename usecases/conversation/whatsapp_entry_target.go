package conversation_usecase

import (
	"errors"

	"vozko/domain/lead"
	wce "vozko/domain/whatsapp_campaign_entry"
)

var ErrEntryWorkspaceUnresolved = errors.New("unable to resolve workspace for whatsapp entry")

func whatsappEntryTarget(
	entries wce.Repository,
	leads lead.Repository,
	entryID string,
) (leadID, leadNumber, businessPhoneID, workspaceID string, err error) {
	entry, err := entries.FindByID(entryID)
	if err != nil {
		return "", "", "", "", err
	}

	campaign, cErr := entries.GetCampaignForEntry(entryID)
	if cErr != nil || campaign == nil {
		return "", "", "", "", ErrEntryWorkspaceUnresolved
	}

	leadRecord, err := leads.FindByID(campaign.WorkspaceID, entry.LeadID)
	if err != nil {
		return "", "", "", "", err
	}

	return entry.LeadID, leadRecord.Number, campaign.BusinessPhoneID, campaign.WorkspaceID, nil
}
