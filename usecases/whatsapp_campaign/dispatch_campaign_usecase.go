package whatsapp_campaign_usecase

import (
	"fmt"
	"log"

	"vozko/domain/cache"
	"vozko/domain/campaign"
	"vozko/domain/messaging"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/usecases/campaignqueue"
)

type dispatchCampaignUseCase struct {
	MessageQueuePub   messaging.MessageQueuePub
	CampaignRepo      wc.Repository
	EntryRepo         wce.Repository
	messageConsumerUC wc.MessageConsumerUseCase
	shared            cache.SharedState
	dispatcher        *campaignqueue.Dispatcher
}

func NewDispatchCampaignUseCase(
	messageQueuePub messaging.MessageQueuePub,
	campaignRepo wc.Repository,
	entryRepo wce.Repository,
	messageConsumerUC wc.MessageConsumerUseCase,
	shared cache.SharedState,
) wc.DispatchCampaignUseCase {
	return &dispatchCampaignUseCase{
		MessageQueuePub:   messageQueuePub,
		CampaignRepo:      campaignRepo,
		EntryRepo:         entryRepo,
		messageConsumerUC: messageConsumerUC,
		shared:            shared,
		dispatcher:        campaignqueue.NewDispatcher(messageQueuePub, shared, wc.QueueNamespace),
	}
}

func (c *dispatchCampaignUseCase) Dispatch(input wc.DispatchCampaignInput) error {
	action := input.Action
	if action == "" {
		action = wc.CampaignActionStart
	}

	if input.CampaignID == "" {
		return wc.ErrDispatchCampaignIDRequired
	}

	if c.CampaignRepo == nil {
		return fmt.Errorf("whatsapp campaign dispatch use case: campaign repository is not configured")
	}

	campaignItem, err := c.CampaignRepo.FindByID(input.CampaignID)
	if err != nil {
		return err
	}
	if campaignItem == nil {
		return fmt.Errorf("whatsapp campaign %s not found", input.CampaignID)
	}

	currentStatus := campaignItem.Status

	transition, err := campaign.ResolveTransition(currentStatus, action)
	if err != nil {
		return err
	}
	targetStatus, revertStatus := transition.Target, transition.Revert
	if !transition.FansOutWork {
		input.Entries = nil
	}

	if targetStatus != "" {
		updated, err := c.CampaignRepo.UpdateStatus(input.CampaignID, targetStatus, currentStatus)
		if err != nil {
			return err
		}
		if !updated {
			return campaign.SwapFailure(action)
		}

		if c.messageConsumerUC != nil {
			switch action {
			case wc.CampaignActionStart:
				if currentStatus == wc.CampaignStatusPaused {
					if c.messageConsumerUC.IsSubscribed(input.CampaignID) {
						if err := c.messageConsumerUC.ResumeCampaignConsumer(input.CampaignID); err != nil {
							log.Printf("failed to resume whatsapp campaign consumer: %v", err)
							return err
						}
					} else {
						if err := c.messageConsumerUC.SubscribeToCampaign(input.CampaignID); err != nil {
							if _, revertErr := c.CampaignRepo.UpdateStatus(input.CampaignID, revertStatus, targetStatus); revertErr != nil {
								return fmt.Errorf("failed to subscribe to campaign: %w (status revert failed: %v)", err, revertErr)
							}
							return fmt.Errorf("failed to subscribe to campaign: %w", err)
						}
					}
					return nil
				}

				if err := c.messageConsumerUC.SubscribeToCampaign(input.CampaignID); err != nil {
					if _, revertErr := c.CampaignRepo.UpdateStatus(input.CampaignID, revertStatus, targetStatus); revertErr != nil {
						return fmt.Errorf("failed to subscribe to campaign: %w (status revert failed: %v)", err, revertErr)
					}
					return fmt.Errorf("failed to subscribe to campaign: %w", err)
				}

				pendingEntries, err := c.EntryRepo.ListByStatus(input.CampaignID, wce.SendStatusPending, 500000)
				if err != nil {
					return fmt.Errorf("failed to list pending entries: %w", err)
				}

				for _, entry := range pendingEntries {
					input.Entries = append(input.Entries, wc.DispatchEntry{
						EntryID:     entry.ID,
						PhoneNumber: entry.Lead.Number,
					})
				}

			case wc.CampaignActionPause:
				if err := c.messageConsumerUC.PauseCampaignConsumer(input.CampaignID); err != nil {
					log.Printf("failed to pause whatsapp campaign consumer: %v", err)
				}

			case wc.CampaignActionStop:
				if err := c.messageConsumerUC.StopCampaignConsumer(input.CampaignID); err != nil {
					log.Printf("failed to stop whatsapp campaign consumer: %v", err)
				}
			}
		}
	}

	messages := make([]campaignqueue.Message, 0, len(input.Entries))
	for _, entry := range input.Entries {
		if entry.PhoneNumber == "" {
			return wc.ErrDispatchPhoneNumbersRequired
		}
		messages = append(messages, campaignqueue.Message{
			CampaignID:  input.CampaignID,
			EntryID:     entry.EntryID,
			PhoneNumber: entry.PhoneNumber,
		})
	}

	if err := c.dispatcher.Enqueue(input.CampaignID, messages); err != nil {
		if targetStatus != "" {
			if _, revertErr := c.CampaignRepo.UpdateStatus(input.CampaignID, revertStatus, targetStatus); revertErr != nil {
				return fmt.Errorf("failed to publish whatsapp campaign dispatch payload: %w (status revert failed: %v)", err, revertErr)
			}
		}
		return err
	}

	return nil
}
