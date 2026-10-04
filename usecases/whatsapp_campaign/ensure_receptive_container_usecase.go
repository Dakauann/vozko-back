package whatsapp_campaign_usecase

import (
	"errors"
	"time"

	wc "vozko/domain/whatsapp_campaign"
)

type ensureReceptiveContainerUseCase struct {
	repo wc.Repository
}

func NewEnsureReceptiveContainerUseCase(repo wc.Repository) wc.EnsureReceptiveContainerUseCase {
	return &ensureReceptiveContainerUseCase{repo: repo}
}

func (uc *ensureReceptiveContainerUseCase) Execute(workspaceID, businessPhoneID, displayPhoneNumber string) (*wc.Campaign, bool, error) {
	existing, err := uc.repo.FindLatestOrganicByBusinessPhone(workspaceID, businessPhoneID)
	if err == nil && existing != nil {
		return existing, false, nil
	}
	if err != nil && !errors.Is(err, wc.ErrCampaignNotFound) {
		return nil, false, err
	}
	container := wc.NewReceptiveContainer(workspaceID, businessPhoneID, displayPhoneNumber, time.Now().UTC())
	if err := uc.repo.Create(container); err != nil {
		return nil, false, err
	}
	return container, true, nil
}
