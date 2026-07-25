package whatsapp_campaign_usecase

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"vozko/domain/cache"
	"vozko/domain/messaging"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type dispatchCampaignUseCase struct {
	MessageQueuePub   messaging.MessageQueuePub
	CampaignRepo      wc.Repository
	EntryRepo         wce.Repository
	messageConsumerUC wc.MessageConsumerUseCase
	shared            cache.SharedState
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

	var (
		targetStatus wc.Status
		revertStatus wc.Status
	)

	switch action {
	case wc.CampaignActionStart:
		switch currentStatus {
		case wc.CampaignStatusRunning:
			return wc.ErrDispatchCampaignAlreadyRunning
		case wc.CampaignStatusPaused, wc.CampaignStatusStopped, wc.CampaignStatusCompleted:
			targetStatus = wc.CampaignStatusRunning
			revertStatus = currentStatus
		default:
			return wc.ErrCampaignStatusInvalid
		}
	case wc.CampaignActionPause:
		input.Entries = nil
		if currentStatus != wc.CampaignStatusRunning {
			return wc.ErrDispatchCampaignNotRunning
		}
		targetStatus = wc.CampaignStatusPaused
		revertStatus = currentStatus
	case wc.CampaignActionStop:
		input.Entries = nil
		switch currentStatus {
		case wc.CampaignStatusStopped:
			return wc.ErrDispatchCampaignAlreadyStopped
		case wc.CampaignStatusRunning, wc.CampaignStatusPaused:
			targetStatus = wc.CampaignStatusStopped
			revertStatus = currentStatus
		default:
			return wc.ErrCampaignStatusInvalid
		}
	default:
		return wc.ErrDispatchActionRequired
	}

	if targetStatus != "" {
		updated, err := c.CampaignRepo.UpdateStatus(input.CampaignID, targetStatus, currentStatus)
		if err != nil {
			return err
		}
		if !updated {
			switch action {
			case wc.CampaignActionStart:
				return wc.ErrDispatchCampaignAlreadyRunning
			case wc.CampaignActionPause:
				return wc.ErrDispatchCampaignNotRunning
			case wc.CampaignActionStop:
				return wc.ErrDispatchCampaignAlreadyStopped
			default:
				return wc.ErrCampaignStatusInvalid
			}
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

	for _, entry := range input.Entries {
		if entry.PhoneNumber == "" {
			return wc.ErrDispatchPhoneNumbersRequired
		}

		payload, err := json.Marshal(wc.DispatchQueueMessage{
			CampaignID:  input.CampaignID,
			EntryID:     entry.EntryID,
			PhoneNumber: entry.PhoneNumber,
		})

		if err != nil {
			return fmt.Errorf("failed to marshal whatsapp campaign dispatch payload: %w", err)
		}

		queueMessage := messaging.QueueMessage{
			Topic:   fmt.Sprintf("%s.%s", wc.WhatsAppCampaignDispatchTopic, input.CampaignID),
			Payload: payload,
		}

		if err := c.MessageQueuePub.Publish(queueMessage.Topic, queueMessage.Payload); err != nil {
			if targetStatus != "" {
				if _, revertErr := c.CampaignRepo.UpdateStatus(input.CampaignID, revertStatus, targetStatus); revertErr != nil {
					return fmt.Errorf("failed to publish whatsapp campaign dispatch payload: %w (status revert failed: %v)", err, revertErr)
				}
			}
			return err
		}
	}

	if c.shared != nil && len(input.Entries) > 0 {
		key := "campaign:whatsapp:remaining:" + input.CampaignID
		_ = c.shared.SetString(key, strconv.Itoa(len(input.Entries)), 72*time.Hour)
	}

	return nil
}
