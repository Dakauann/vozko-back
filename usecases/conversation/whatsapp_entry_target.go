package conversation_usecase

import (
	"errors"

	"vozko/domain/lead"
	wce "vozko/domain/whatsapp_campaign_entry"
)

// ErrEntryWorkspaceUnresolved means the entry exists but its campaign — and so
// its workspace and sending number — could not be read. Sending is impossible
// either way, and saying which of the two failed is what makes the log usable.
var ErrEntryWorkspaceUnresolved = errors.New("unable to resolve workspace for whatsapp entry")

// whatsappEntryTarget answers "where does a message to this entry go".
//
// The send path asked this twice, from two services, in two functions that were
// the same four repository calls and the same error strings written out
// separately — one returning three values, the other two. Different return
// shapes were the only difference, which is not a reason for two copies of the
// lookup underneath them.
//
// It takes its ports as arguments rather than hanging off either service: it
// belongs to neither, and passing them keeps it honest about being a plain
// query over two domain ports rather than a third service with state.
func whatsappEntryTarget(
	entries wce.Repository,
	leads lead.Repository,
	entryID string,
) (leadID, leadNumber, businessPhoneID, workspaceID string, err error) {
	entry, err := entries.FindByID(entryID)
	if err != nil {
		return "", "", "", "", err
	}

	// The campaign carries the workspace AND the number to send from, so a
	// missing one is fatal here even though the entry itself resolved.
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
