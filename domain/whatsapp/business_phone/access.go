package businessphone

import (
	"strings"

	"vozko/domain/workspace_phone_access"
)

type AccessGrantReader interface {
	HasAccess(workspaceID, phoneID string) (bool, error)
}

func CanWorkspaceSendFrom(
	workspaceID, phoneID string,
	phone *WhatsAppBusinessPhoneNumber,
	grants AccessGrantReader,
) (bool, error) {
	if phone != nil && strings.TrimSpace(phone.OwnerWorkspaceID) != "" {
		return phone.BelongsToWorkspace(workspaceID), nil
	}

	if grants == nil || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(phoneID) == "" {
		return false, nil
	}

	return grants.HasAccess(workspaceID, phoneID)
}

var _ AccessGrantReader = (workspace_phone_access.Repository)(nil)
