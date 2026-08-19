package businessphone

import (
	"strings"

	"vozko/domain/workspace_phone_access"
)

// AccessGrantReader is the narrow slice of the grant table this rule needs.
//
// Declared here rather than importing the repository wholesale so the rule can
// be answered by anything that can say yes or no — including a test fake.
type AccessGrantReader interface {
	HasAccess(workspaceID, phoneID string) (bool, error)
}

// CanWorkspaceSendFrom answers whether a workspace may send from a number.
//
// The rule has one asymmetry worth stating: an OWNED phone is decided by
// ownership alone. A grant cannot override a foreign owner, or the grant table
// would become a way to send from somebody else's number. Only an unowned phone
// — one being operated on a workspace's behalf before ownership is assigned —
// falls through to the grants.
//
// A nil reader answers false. Fail closed: the alternative is that a mis-wired
// container silently permits every workspace to send from every number.
//
// Promoted out of usecases/whatsapp_campaign, where it was package-private and
// therefore unreachable by the second caller that needed exactly this rule.
// Duplicating it would have meant two answers to one question.
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

// Compile-time proof that the real grant repository satisfies the narrow port,
// so a change to it is caught here rather than in the container.
var _ AccessGrantReader = (workspace_phone_access.Repository)(nil)
