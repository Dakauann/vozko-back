package whatsapp_campaign_usecase

import (
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

const recentEntriesLimit = 100

type getCampaignUseCase struct {
	campaignRepo wc.Repository
	entryRepo    wce.Repository
}

func NewGetCampaignUseCase(campaignRepo wc.Repository, entryRepo wce.Repository) wc.GetCampaignUseCase {
	return &getCampaignUseCase{campaignRepo: campaignRepo, entryRepo: entryRepo}
}

func (uc *getCampaignUseCase) Execute(campaignID string) (*wc.Campaign, error) {
	if campaignID == "" {
		return nil, wc.ErrCampaignNotFound
	}

	existing, err := uc.campaignRepo.FindByID(campaignID)
	if err != nil {
		return nil, err
	}

	counts, err := uc.entryRepo.CountByStatus(campaignID)
	if err != nil {
		return nil, err
	}
	existing.Metrics = wc.NewCampaignMetrics(counts)

	recentEntries, err := uc.entryRepo.ListRecentlyUpdated(campaignID, recentEntriesLimit)
	if err != nil {
		return nil, err
	}
	existing.RecentEntries = recentEntries

	return existing, nil
}
