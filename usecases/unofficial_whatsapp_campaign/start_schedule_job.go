package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"log"
	"time"

	"vozko/domain/campaign"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

const scheduleStartBatchSize = 200

type scheduleStartJob struct {
	campaigns uwc.Repository
	dispatch  uwc.DispatchCampaignUseCase
}

func NewScheduleStartJob(
	campaigns uwc.Repository,
	dispatch uwc.DispatchCampaignUseCase,
) uwc.StartScheduleJob {
	return &scheduleStartJob{campaigns: campaigns, dispatch: dispatch}
}

func (j *scheduleStartJob) StartScheduledCampaigns() error {
	due, err := j.campaigns.ListScheduledToStart(time.Now().UTC(), scheduleStartBatchSize)
	if err != nil {
		return err
	}

	for _, camp := range due {
		if camp == nil || camp.ID == "" {
			continue
		}
		err := j.dispatch.Dispatch(context.Background(), uwc.DispatchCampaignInput{
			CampaignID: camp.ID,
			Action:     campaign.ActionStart,
		})
		if err == nil {
			continue
		}
		if errors.Is(err, campaign.ErrAlreadyRunning) {
			continue
		}
		log.Printf("[unofficial-whatsapp-campaign] scheduled start failed for %s: %v", camp.ID, err)
	}
	return nil
}
