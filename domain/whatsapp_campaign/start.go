package whatsapp_campaign

import "errors"

var (
	ErrCampaignNoSubscription = errors.New("whatsapp campaign start: the workspace has no active subscription")
	ErrCampaignNoNumbers      = errors.New("whatsapp campaign start: the campaign has no phone numbers to process")
	ErrCampaignAllProcessed   = errors.New("whatsapp campaign start: all phone numbers have already been processed")
)

func (c *Campaign) Startable() error {
	if c.Metrics == nil || c.Metrics.TotalNumbers == 0 {
		return ErrCampaignNoNumbers
	}
	if c.Metrics.Pending == 0 && c.Metrics.Processed == c.Metrics.TotalNumbers {
		return ErrCampaignAllProcessed
	}
	return nil
}
