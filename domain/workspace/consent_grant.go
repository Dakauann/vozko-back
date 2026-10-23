package workspace

import (
	"errors"
	"fmt"
)

const (
	CapabilityLeadConsent     CapabilityKey = "leads.consent"
	CodeConsentGrantForbidden               = "lead_consent_grant_forbidden"

	CapabilityGoogleSheetsView CapabilityKey = "google_sheets.view"
	CodeSheetBindingForbidden                = "google_sheet_binding_forbidden"
)

var (
	ErrConsentGrantForbidden = errors.New("workspace: granting lead consent needs the leads.consent capability")
	ErrSheetBindingForbidden = errors.New("workspace: giving an agent a google sheet needs the google_sheets.view capability")
)

type Requester struct {
	UserID      string
	SystemAdmin bool
}

func RequireConsentGrant(checker PermissionChecker, workspaceID string, by Requester) error {
	return requireCapability(checker, workspaceID, by, CapabilityLeadConsent, ErrConsentGrantForbidden)
}

func RequireSheetBinding(checker PermissionChecker, workspaceID string, by Requester) error {
	return requireCapability(checker, workspaceID, by, CapabilityGoogleSheetsView, ErrSheetBindingForbidden)
}

func requireCapability(checker PermissionChecker, workspaceID string, by Requester, key CapabilityKey, forbidden error) error {
	needed, err := CapabilitiesRequire([]CapabilityKey{key})
	if err != nil {
		return fmt.Errorf("%w: %w", forbidden, err)
	}
	if !HoldsAll(checker, workspaceID, by.UserID, by.SystemAdmin, needed) {
		return forbidden
	}
	return nil
}
