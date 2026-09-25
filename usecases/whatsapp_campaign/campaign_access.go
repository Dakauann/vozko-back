package whatsapp_campaign_usecase

import (
	"strings"

	wc "vozko/domain/whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
)

type campaignOwnership struct {
	get wc.GetCampaignUseCase
}

func NewCampaignAccessUseCase(get wc.GetCampaignUseCase) wc.CampaignAccessUseCase {
	return &campaignOwnership{get: get}
}

func (uc *campaignOwnership) Owned(workspaceID string, departments *wd.DepartmentFilter, campaignID string) (*wc.Campaign, error) {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(campaignID) == "" {
		return nil, wc.ErrCampaignNotFound
	}
	c, err := uc.get.Execute(campaignID)
	if err != nil {
		return nil, err
	}
	if c == nil || c.WorkspaceID != workspaceID || !departments.Allows(c.DepartmentID) {
		return nil, wc.ErrCampaignNotFound
	}
	return c, nil
}
