package whatsapp_campaign_usecase

import "context"

type SmartCampaigns interface {
	Manages(campaignID string) (bool, error)
}

type SmartStarter interface {
	SmartCampaigns
	Start(ctx context.Context, campaignID string) error
}

func (c *messageConsumerUseCase) SetSmartCampaigns(smart SmartCampaigns) {
	c.smart = smart
}

func (s campaignStatusStore) ownedByWaves(campaignID string) (bool, error) {
	if s.owner == nil || s.owner.smart == nil {
		return false, nil
	}
	return s.owner.smart.Manages(campaignID)
}
