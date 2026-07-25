package businessphone

import "time"

// Dialog360ProvisionInput carries the data needed to provision a 360dialog
// channel from a number that completed Embedded Signup on our own domain
// (Partner Hosted). The Meta ids come from the signup; the owner context binds
// the resulting phone to a workspace.
type Dialog360ProvisionInput struct {
	WABAExternalID     string
	PhoneNumberID      string
	ClientName         string
	ClientEmail        string
	OwnerWorkspaceID   string
	OwnerAssignedBy    string
	DisplayPhoneNumber string
}

// Dialog360Onboarder is the delivery-facing port for Partner Hosted 360dialog
// onboarding. The infra service implements it; HTTP handlers depend on this
// interface, not the concrete type, so the layering stays clean.
type Dialog360Onboarder interface {
	// StartProvisioning writes the phone row first, then runs the handover. On
	// handover failure it returns the phone (flipped to StatusOnboardingFailed)
	// plus the error, so the failure is always visible and retryable.
	StartProvisioning(in Dialog360ProvisionInput) (*WhatsAppBusinessPhoneNumber, error)
	// Retry re-runs a failed handover for a previously persisted number. It is
	// scoped to ownerWorkspaceID so a workspace can only retry its own numbers.
	Retry(phoneID, ownerWorkspaceID string) (*WhatsAppBusinessPhoneNumber, error)
	// Finalize is invoked on the channel_live webhook to mint the key and connect.
	Finalize(channelID string, now time.Time) error
}
