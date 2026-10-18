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
	schedules campaign.ScheduleClaimer
}

func NewScheduleStartJob(
	campaigns uwc.Repository,
	dispatch uwc.DispatchCampaignUseCase,
	schedules campaign.ScheduleClaimer,
) uwc.StartScheduleJob {
	return &scheduleStartJob{campaigns: campaigns, dispatch: dispatch, schedules: schedules}
}

func (j *scheduleStartJob) StartScheduledCampaigns() error {
	now := time.Now().UTC()
	due, err := j.campaigns.ListScheduledToStart(now, scheduleStartBatchSize)
	if err != nil {
		return err
	}

	for _, camp := range due {
		if camp == nil || camp.ID == "" {
			continue
		}
		claimed, err := j.schedules.ClaimScheduledStart(camp.ID, now)
		if err != nil {
			log.Printf("[unofficial-whatsapp-campaign] could not claim the schedule of %s: %v", camp.ID, err)
			continue
		}
		if !claimed {
			continue
		}
		err = j.dispatch.Dispatch(context.Background(), uwc.DispatchCampaignInput{
			CampaignID: camp.ID,
			Action:     campaign.ActionStart,
		})
		if err == nil || errors.Is(err, campaign.ErrAlreadyRunning) {
			continue
		}
		log.Printf("[unofficial-whatsapp-campaign] scheduled start failed for %s: %v", camp.ID, err)
	}
	return nil
}
