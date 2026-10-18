package whatsapp_campaign_usecase

import (
	"errors"
	"fmt"
	"time"

	wc "vozko/domain/whatsapp_campaign"
)

const scheduleStartBatchSize = 200

type ScheduleStartJob struct {
	dispatchCampaignUseCase wc.DispatchCampaignUseCase
	whatsappCampaingsRepo   wc.Repository
	schedules               wc.ScheduleClaimer
}

func NewScheduleStartJob(dispatchCampaignUseCase wc.DispatchCampaignUseCase, whatsappCampaingsRepo wc.Repository, schedules wc.ScheduleClaimer) wc.StartScheduleJob {
	return &ScheduleStartJob{
		dispatchCampaignUseCase: dispatchCampaignUseCase,
		whatsappCampaingsRepo:   whatsappCampaingsRepo,
		schedules:               schedules,
	}
}

func (j *ScheduleStartJob) StartScheduledWhatsappCampaigns() error {
	now := time.Now().UTC()
	campaigns, err := j.whatsappCampaingsRepo.ListScheduledToStart(now, scheduleStartBatchSize)
	if err != nil {
		return err
	}

	var errs []error
	for _, campaign := range campaigns {
		if campaign == nil || campaign.ID == "" {
			continue
		}
		claimed, err := j.schedules.ClaimScheduledStart(campaign.ID, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("claim the schedule of %s: %w", campaign.ID, err))
			continue
		}
		if !claimed {
			continue
		}
		err = j.dispatchCampaignUseCase.Dispatch(wc.DispatchCampaignInput{
			CampaignID: campaign.ID,
			Action:     wc.CampaignActionStart,
		})
		if err != nil && !errors.Is(err, wc.ErrDispatchCampaignAlreadyRunning) {
			errs = append(errs, fmt.Errorf("scheduled start of %s: %w", campaign.ID, err))
		}
	}

	return errors.Join(errs...)
}

var _ wc.StartScheduleJob = (*ScheduleStartJob)(nil)
