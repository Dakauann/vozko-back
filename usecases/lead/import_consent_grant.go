package lead_usecase

import (
	"fmt"

	"vozko/domain/leadimport"
	"vozko/domain/workspace"
)

func (i *Import) refuseUngrantedConsent(a Actor, s leadimport.Settings) error {
	if !s.GrantsConsent() {
		return nil
	}
	return i.consentGrant(a)
}

func (i *Import) consentGrant(a Actor) error {
	by := workspace.Requester{UserID: a.UserID, SystemAdmin: a.IsAdmin}
	if err := workspace.RequireConsentGrant(i.deps.Permissions, a.WorkspaceID, by); err != nil {
		return fmt.Errorf("%w: %w", leadimport.ErrForbidden, err)
	}
	return nil
}

func (i *Import) catalogField(a Actor, f leadimport.Field) ImportField {
	if leadimport.GrantsConsentField(f) {
		f.Requires = workspace.ActionConsent
		return ImportField{Field: f, Allowed: i.consentGrant(a) == nil}
	}
	return ImportField{Field: f, Allowed: f.Requires == "" || i.viewers.allowed(a, f.Requires)}
}
