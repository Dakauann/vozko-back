package whatsapp_campaign

import "errors"

var (
	ErrDispatchPhoneNumbersRequired   = errors.New("whatsapp campaign dispatch requires at least one phone number")
	ErrDispatchCampaignIDRequired     = errors.New("whatsapp campaign dispatch requires a campaign id")
	ErrDispatchActionRequired         = errors.New("whatsapp campaign dispatch requires a valid action")
	ErrDispatchCampaignAlreadyRunning = errors.New("whatsapp campaign is already running")
	ErrDispatchCampaignNotRunning     = errors.New("whatsapp campaign is not running")
	ErrDispatchCampaignAlreadyStopped = errors.New("whatsapp campaign is already stopped")
	ErrCampaignClearCodeInvalid       = errors.New("invalid clear history confirmation code")
	ErrCampaignClearNotAllowed        = errors.New("whatsapp campaign clear history not allowed while running")
)
