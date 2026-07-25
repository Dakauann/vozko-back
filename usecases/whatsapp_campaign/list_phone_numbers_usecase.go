package whatsapp_campaign_usecase

import (
	"vozko/domain/shared"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type listEntriesUseCase struct {
	campaignRepo wc.Repository
	entryRepo    wce.Repository
}

func NewListEntriesUseCase(campaignRepo wc.Repository, entryRepo wce.Repository) wc.ListEntriesUseCase {
	return &listEntriesUseCase{
		campaignRepo: campaignRepo,
		entryRepo:    entryRepo,
	}
}

func (uc *listEntriesUseCase) Execute(input wce.ListEntriesInput) (*shared.PaginatedResult[*wce.EntryWithLead], error) {
	if input.CampaignID == "" {
		return nil, wc.ErrCampaignNotFound
	}

	campaign, err := uc.campaignRepo.FindByID(input.CampaignID)
	if err != nil {
		return nil, err
	}

	input.BusinessPhoneID = campaign.BusinessPhoneID

	return uc.entryRepo.ListEntriesWithLeads(input)
}
