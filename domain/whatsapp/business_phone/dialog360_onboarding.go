package businessphone

import "time"

type Dialog360ProvisionInput struct {
	WABAExternalID     string
	PhoneNumberID      string
	ClientName         string
	ClientEmail        string
	OwnerWorkspaceID   string
	OwnerAssignedBy    string
	DisplayPhoneNumber string
}

type Dialog360Onboarder interface {
	StartProvisioning(in Dialog360ProvisionInput) (*WhatsAppBusinessPhoneNumber, error)
	Retry(phoneID, ownerWorkspaceID string) (*WhatsAppBusinessPhoneNumber, error)
	Finalize(channelID string, now time.Time) error
}
