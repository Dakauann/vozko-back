package whatsapp_campaign_usecase

import (
	"fmt"

	wc "vozko/domain/whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
	"vozko/domain/workspace/workspace_plan"
)

type StartCampaignDeps struct {
	Access       wc.CampaignAccessUseCase
	Subscription workspace_plan.EnsureActiveWorkspaceSubscriptionUseCase
	Dispatch     wc.DispatchCampaignUseCase
}

type startCampaign struct{ deps StartCampaignDeps }

func NewStartCampaignUseCase(deps StartCampaignDeps) wc.StartCampaignUseCase {
	return &startCampaign{deps: deps}
}

func (uc *startCampaign) Start(workspaceID string, departments *wd.DepartmentFilter, campaignID string) (*wc.Campaign, error) {
	return uc.start(workspaceID, departments, campaignID, false)
}

func (uc *startCampaign) StartReviewed(workspaceID string, departments *wd.DepartmentFilter, campaignID string) (*wc.Campaign, error) {
	return uc.start(workspaceID, departments, campaignID, true)
}

func (uc *startCampaign) start(workspaceID string, departments *wd.DepartmentFilter, campaignID string, reviewed bool) (*wc.Campaign, error) {
	campaign, err := uc.deps.Access.Owned(workspaceID, departments, campaignID)
	if err != nil {
		return nil, err
	}
	if _, err := uc.deps.Subscription.Execute(campaign.WorkspaceID); err != nil {
		return nil, fmt.Errorf("%w: %v", wc.ErrCampaignNoSubscription, err)
	}
	if err := campaign.Startable(); err != nil {
		return nil, err
	}
	if err := uc.deps.Dispatch.Dispatch(wc.DispatchCampaignInput{CampaignID: campaign.ID, Action: wc.CampaignActionStart, Reviewed: reviewed}); err != nil {
		return nil, err
	}
	return campaign, nil
}
