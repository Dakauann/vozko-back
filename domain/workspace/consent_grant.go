package workspace

import (
	"errors"
	"fmt"
)

const (
	CapabilityLeadConsent     CapabilityKey = "leads.consent"
	CodeConsentGrantForbidden               = "lead_consent_grant_forbidden"
)

var ErrConsentGrantForbidden = errors.New("workspace: granting lead consent needs the leads.consent capability")

type Requester struct {
	UserID      string
	SystemAdmin bool
}

func RequireConsentGrant(checker PermissionChecker, workspaceID string, by Requester) error {
	needed, err := CapabilitiesRequire([]CapabilityKey{CapabilityLeadConsent})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrConsentGrantForbidden, err)
	}
	if !HoldsAll(checker, workspaceID, by.UserID, by.SystemAdmin, needed) {
		return ErrConsentGrantForbidden
	}
	return nil
}
