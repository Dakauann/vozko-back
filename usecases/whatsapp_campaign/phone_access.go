package whatsapp_campaign_usecase

import (
	"strings"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/workspace_phone_access"
)

func hasTemporaryCampaignBusinessPhoneAccess(
	workspaceID, phoneID string,
	phone *businessphone.WhatsAppBusinessPhoneNumber,
	phoneAccessRepo workspace_phone_access.Repository,
) (bool, error) {
	if phone != nil {
		if strings.TrimSpace(phone.OwnerWorkspaceID) != "" {
			return phone.BelongsToWorkspace(workspaceID), nil
		}
	}

	if phoneAccessRepo == nil || workspaceID == "" || phoneID == "" {
		return false, nil
	}

	return phoneAccessRepo.HasAccess(workspaceID, phoneID)
}
