package whatsapp_campaign_usecase

import (
	"errors"
	"time"
	wc "vozko/domain/whatsapp_campaign"
)

const scheduleStartBatchSize = 200

type ScheduleStartJob struct {
	dispatchCampaignUseCase wc.DispatchCampaignUseCase
	whatsappCampaingsRepo   wc.Repository
}

func NewScheduleStartJob(dispatchCampaignUseCase wc.DispatchCampaignUseCase, whatsappCampaingsRepo wc.Repository) wc.StartScheduleJob {
	return &ScheduleStartJob{
		dispatchCampaignUseCase: dispatchCampaignUseCase,
		whatsappCampaingsRepo:   whatsappCampaingsRepo,
	}
}

func (j *ScheduleStartJob) StartScheduledWhatsappCampaigns() error {
	now := time.Now().UTC()
	campaigns, err := j.whatsappCampaingsRepo.ListScheduledToStart(now, scheduleStartBatchSize)
	if err != nil {
		return err
	}

	for _, campaign := range campaigns {
		if campaign == nil || campaign.ID == "" {
			continue
		}

		err = j.dispatchCampaignUseCase.Dispatch(wc.DispatchCampaignInput{
			CampaignID: campaign.ID,
			Action:     wc.CampaignActionStart,
		})
		if err != nil {
			if errors.Is(err, wc.ErrDispatchCampaignAlreadyRunning) {
				continue
			}
			return err
		}
	}

	return nil
}

var _ wc.StartScheduleJob = (*ScheduleStartJob)(nil)
