package whatsapp_campaign

import (
	"errors"

	"vozko/domain/campaign"
)

var (
	ErrCampaignNoSubscription = errors.New("whatsapp campaign start: the workspace has no active subscription")
	ErrCampaignNoNumbers      = campaign.ErrNothingToSend
	ErrCampaignAllProcessed   = campaign.ErrAlreadySent
)

func (c *Campaign) Startable() error {
	return c.Metrics.Startable()
}
