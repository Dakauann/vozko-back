package whatsapp_campaign

import (
	"errors"

	"vozko/domain/campaign"
)

var (
	ErrDispatchPhoneNumbersRequired = errors.New("whatsapp campaign dispatch requires at least one phone number")
	ErrDispatchCampaignIDRequired   = errors.New("whatsapp campaign dispatch requires a campaign id")

	// The lifecycle refusals ARE the kernel's, not copies of them.
	//
	// Aliased rather than re-declared so errors.Is holds in both directions: the
	// HTTP layer matches on these names, the shared transition resolver returns
	// the kernel's, and a re-declared sentinel would make the two silently fail
	// to match — turning "already running" into a 500.
	ErrDispatchActionRequired         = campaign.ErrActionInvalid
	ErrDispatchCampaignAlreadyRunning = campaign.ErrAlreadyRunning
	ErrDispatchCampaignNotRunning     = campaign.ErrNotRunning
	ErrDispatchCampaignAlreadyStopped = campaign.ErrAlreadyStopped

	ErrCampaignClearCodeInvalid = errors.New("invalid clear history confirmation code")
	ErrCampaignClearNotAllowed  = errors.New("whatsapp campaign clear history not allowed while running")
)
