package whatsapp_campaign_usecase

import (
	"fmt"
	"log"
	"time"

	wc "vozko/domain/whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
	"vozko/domain/workspace/workspace_plan"
)

type StartCampaignDeps struct {
	Access       wc.CampaignAccessUseCase
	Subscription workspace_plan.EnsureActiveWorkspaceSubscriptionUseCase
	Dispatch     wc.DispatchCampaignUseCase
	Schedules    wc.ScheduleClaimer
}

type startCampaign struct{ deps StartCampaignDeps }

func NewStartCampaignUseCase(deps StartCampaignDeps) wc.StartCampaignUseCase {
	return &startCampaign{deps: deps}
}

func (uc *startCampaign) Start(workspaceID string, departments *wd.DepartmentFilter, campaignID string) (*wc.Campaign, error) {
	return uc.start(workspaceID, departments, campaignID, false, "")
}

func (uc *startCampaign) StartConfirmed(workspaceID string, departments *wd.DepartmentFilter, campaignID, confirmedFingerprint string) (*wc.Campaign, error) {
	return uc.start(workspaceID, departments, campaignID, false, confirmedFingerprint)
}

func (uc *startCampaign) StartReviewed(workspaceID string, departments *wd.DepartmentFilter, campaignID string) (*wc.Campaign, error) {
	return uc.start(workspaceID, departments, campaignID, true, "")
}

func (uc *startCampaign) start(workspaceID string, departments *wd.DepartmentFilter, campaignID string, reviewed bool, confirmedFingerprint string) (*wc.Campaign, error) {
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
	if err := uc.deps.Dispatch.Dispatch(wc.DispatchCampaignInput{CampaignID: campaign.ID, Action: wc.CampaignActionStart, Reviewed: reviewed, ConfirmedFingerprint: confirmedFingerprint}); err != nil {
		return nil, err
	}
	if uc.deps.Schedules != nil {
		if _, err := uc.deps.Schedules.ClaimScheduledStart(campaign.ID, time.Now().AddDate(10, 0, 0)); err != nil {
			log.Printf("[whatsapp-campaign] started %s but could not consume its schedule: %v", campaign.ID, err)
		}
	}
	return campaign, nil
}
