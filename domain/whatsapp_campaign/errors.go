package whatsapp_campaign

import (
	"errors"

	"vozko/domain/campaign"
)

var (
	ErrDispatchPhoneNumbersRequired = errors.New("whatsapp campaign dispatch requires at least one phone number")
	ErrDispatchCampaignIDRequired   = errors.New("whatsapp campaign dispatch requires a campaign id")

	ErrDispatchActionRequired         = campaign.ErrActionInvalid
	ErrDispatchCampaignAlreadyRunning = campaign.ErrAlreadyRunning
	ErrDispatchCampaignNotRunning     = campaign.ErrNotRunning
	ErrDispatchCampaignAlreadyStopped = campaign.ErrAlreadyStopped

	ErrCampaignClearCodeInvalid = errors.New("invalid clear history confirmation code")
	ErrCampaignClearNotAllowed  = errors.New("whatsapp campaign clear history not allowed while running")
)
