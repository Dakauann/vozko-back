package whatsapp_campaign_usecase

import (
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type deleteCampaignUseCase struct {
	campaignRepo wc.Repository
	entryRepo    wce.Repository
}

func NewDeleteCampaignUseCase(campaignRepo wc.Repository, entryRepo wce.Repository) wc.DeleteCampaignUseCase {
	return &deleteCampaignUseCase{campaignRepo: campaignRepo, entryRepo: entryRepo}
}

func (uc *deleteCampaignUseCase) Execute(campaignID string) error {
	if campaignID == "" {
		return wc.ErrCampaignNotFound
	}

	if _, err := uc.campaignRepo.FindByID(campaignID); err != nil {
		return err
	}

	if err := uc.entryRepo.DeleteByCampaignID(campaignID); err != nil {
		return err
	}

	return uc.campaignRepo.Delete(campaignID)
}
