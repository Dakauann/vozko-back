package unofficial_whatsapp_campaign

import (
	"context"
	"strings"

	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

type campaignAccess struct {
	get uwc.GetCampaignUseCase
}

func NewCampaignAccessUseCase(get uwc.GetCampaignUseCase) uwc.CampaignAccessUseCase {
	return &campaignAccess{get: get}
}

func (uc *campaignAccess) Owned(ctx context.Context, workspaceID string, scope uw.DepartmentScope, campaignID string) (*uwc.Campaign, error) {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(campaignID) == "" {
		return nil, uwc.ErrCampaignNotFound
	}
	c, err := uc.get.Execute(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	if c == nil || c.WorkspaceID != workspaceID {
		return nil, uwc.ErrCampaignNotFound
	}
	department := c.DepartmentID
	if !scope.Allows(&department) {
		return nil, uwc.ErrCampaignNotFound
	}
	return c, nil
}
