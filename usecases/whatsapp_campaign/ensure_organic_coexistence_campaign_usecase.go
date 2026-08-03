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

// Execute preserves the exact behavior previously inlined in the Meta embedded
// signup handler: return the latest organic campaign for the phone, or, when
// none exists (or the lookup errors), create a running organic campaign with
// the same naming/metadata and report created=true.
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
