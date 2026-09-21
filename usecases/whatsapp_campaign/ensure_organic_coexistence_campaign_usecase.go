package whatsapp_campaign_usecase

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	wc "vozko/domain/whatsapp_campaign"
)

type ensureOrganicCoexistenceCampaignUseCase struct {
	repo wc.Repository
}

func NewEnsureOrganicCoexistenceCampaignUseCase(repo wc.Repository) wc.EnsureOrganicCoexistenceCampaignUseCase {
	return &ensureOrganicCoexistenceCampaignUseCase{repo: repo}
}

func (uc *ensureOrganicCoexistenceCampaignUseCase) Execute(workspaceID, businessPhoneID, displayPhoneNumber string) (*wc.Campaign, bool, error) {
	existing, err := uc.repo.FindLatestOrganicByBusinessPhone(workspaceID, businessPhoneID)
	if err == nil && existing != nil {
		return existing, false, nil
	}

	now := time.Now().UTC()
	campaign := &wc.Campaign{
		ID:              uuid.New().String(),
		WorkspaceID:     workspaceID,
		BusinessPhoneID: businessPhoneID,
		Name:            fmt.Sprintf("Organic – %s (coexistence)", displayPhoneNumber),
		Type:            wc.CampaignTypeOrganic,
		Status:          wc.CampaignStatusRunning,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if createErr := uc.repo.Create(campaign); createErr != nil {
		return nil, false, createErr
	}
	return campaign, true, nil
}
