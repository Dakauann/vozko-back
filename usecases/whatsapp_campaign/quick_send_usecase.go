package whatsapp_campaign_usecase

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/google/uuid"

	"vozko/domain/cache"
	"vozko/domain/lead"
	"vozko/domain/messaging"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

var ErrQuickSendBusy = errors.New("whatsapp campaign quick send: campaign is busy, please retry")

const (
	quickSendLockTTL = 30 * time.Second

	remainingCounterTTL = 72 * time.Hour
)

type quickSendUseCase struct {
	campaignRepo      wc.Repository
	entryRepo         wce.Repository
	leadRepo          lead.Repository
	messageQueuePub   messaging.MessageQueuePub
	messageConsumerUC wc.MessageConsumerUseCase
	shared            cache.SharedState
}

func NewQuickSendUseCase(
	campaignRepo wc.Repository,
	entryRepo wce.Repository,
	leadRepo lead.Repository,
	messageQueuePub messaging.MessageQueuePub,
	messageConsumerUC wc.MessageConsumerUseCase,
	shared cache.SharedState,
) wc.QuickSendUseCase {
	return &quickSendUseCase{
		campaignRepo:      campaignRepo,
		entryRepo:         entryRepo,
		leadRepo:          leadRepo,
		messageQueuePub:   messageQueuePub,
		messageConsumerUC: messageConsumerUC,
		shared:            shared,
	}
}

func quickSendLockKey(campaignID string) string {
	return "campaign:whatsapp:quicksend_lock:" + campaignID
}

func remainingCounterKey(campaignID string) string {
	return "campaign:whatsapp:remaining:" + campaignID
}

func (uc *quickSendUseCase) Execute(input wc.QuickSendInput) (*wc.QuickSendOutput, error) {
	if input.CampaignID == "" {
		return nil, wc.ErrDispatchCampaignIDRequired
	}
	if uc.campaignRepo == nil || uc.entryRepo == nil || uc.leadRepo == nil || uc.messageQueuePub == nil {
		return nil, fmt.Errorf("whatsapp campaign quick send: dependencies not configured")
	}

	if uc.shared != nil {
		acquired, err := uc.shared.SetNX(quickSendLockKey(input.CampaignID), "1", quickSendLockTTL)
		if err != nil {
			return nil, fmt.Errorf("whatsapp campaign quick send: failed to acquire lock: %w", err)
		}
		if !acquired {
			return nil, ErrQuickSendBusy
		}
		defer func() {
			if err := uc.shared.Del(quickSendLockKey(input.CampaignID)); err != nil {
				log.Printf("[whatsapp_campaign quick_send] failed to release lock for %s: %v", input.CampaignID, err)
			}
		}()
	} else {
		log.Printf("[whatsapp_campaign quick_send] WARN: shared state is nil, replica-safety degraded")
	}

	campaign, err := uc.campaignRepo.FindByID(input.CampaignID)
	if err != nil {
		return nil, err
	}
	if campaign == nil {
		return nil, wc.ErrCampaignNotFound
	}

	for _, p := range input.PhoneNumbers {
		if lead.NormalizeNumber(p.Number) == "" {
			return nil, wc.ErrCampaignPhoneNumberInvalid
		}
	}

	if len(input.PhoneNumbers) > 0 {
		currentCount, err := uc.entryRepo.CountByCampaignID(input.CampaignID)
		if err != nil {
			return nil, err
		}
		if int(currentCount)+len(input.PhoneNumbers) > wc.MaxCampaignPhoneNumbers {
			return nil, wc.ErrCampaignPhoneNumbersTooMany
		}
	}

	output := &wc.QuickSendOutput{
		CampaignID: campaign.ID,
		Entries:    make([]wc.EntryOutput, 0),
	}

	addedEntries, err := uc.insertEntries(campaign.WorkspaceID, input, output)
	if err != nil {
		return nil, err
	}

	previousStatus := campaign.Status
	wasAlreadyRunning := previousStatus == wc.CampaignStatusRunning

	var toPublish []wce.WhatsAppCampaignEntry
	if wasAlreadyRunning {
		toPublish = addedEntries
	} else {
		pending, err := uc.entryRepo.ListByStatus(input.CampaignID, wce.SendStatusPending, wc.MaxCampaignPhoneNumbers)
		if err != nil {
			return nil, fmt.Errorf("failed to list pending entries: %w", err)
		}
		toPublish = pending
	}

	if len(toPublish) == 0 {
		output.Status = string(campaign.Status)
		return output, nil
	}

	statusTransitioned := false
	if !wasAlreadyRunning {
		switch previousStatus {
		case wc.CampaignStatusPaused, wc.CampaignStatusStopped, wc.CampaignStatusCompleted:
			updated, err := uc.campaignRepo.UpdateStatus(input.CampaignID, wc.CampaignStatusRunning, previousStatus)
			if err != nil {
				return nil, err
			}
			if !updated {
				refreshed, refreshErr := uc.campaignRepo.FindByID(input.CampaignID)
				if refreshErr != nil {
					return nil, refreshErr
				}
				if refreshed == nil || refreshed.Status != wc.CampaignStatusRunning {
					return nil, wc.ErrCampaignStatusInvalid
				}

				campaign = refreshed
				wasAlreadyRunning = true
				toPublish = addedEntries
				if len(toPublish) == 0 {
					output.Status = string(campaign.Status)
					return output, nil
				}
			} else {
				statusTransitioned = true
				campaign.Status = wc.CampaignStatusRunning
			}
		default:
			return nil, wc.ErrCampaignStatusInvalid
		}
	}

	if err := uc.ensureConsumer(input.CampaignID, previousStatus); err != nil {
		if statusTransitioned {
			uc.revertStatus(input.CampaignID, previousStatus)
		}
		return nil, err
	}

	if uc.shared != nil {
		key := remainingCounterKey(input.CampaignID)
		if wasAlreadyRunning {
			if _, err := uc.shared.IncrBy(key, int64(len(toPublish))); err != nil {
				if statusTransitioned {
					uc.revertStatus(input.CampaignID, previousStatus)
				}
				return nil, fmt.Errorf("failed to increment whatsapp campaign remaining counter: %w", err)
			}

			_, _ = uc.shared.Expire(key, remainingCounterTTL)
		} else {
			if err := uc.shared.Del(key); err != nil {
				if statusTransitioned {
					uc.revertStatus(input.CampaignID, previousStatus)
				}
				return nil, fmt.Errorf("failed to clear whatsapp campaign remaining counter: %w", err)
			}
			if err := uc.shared.SetString(key, strconv.Itoa(len(toPublish)), remainingCounterTTL); err != nil {
				if statusTransitioned {
					uc.revertStatus(input.CampaignID, previousStatus)
				}
				return nil, fmt.Errorf("failed to seed whatsapp campaign remaining counter: %w", err)
			}
		}
	}

	topic := fmt.Sprintf("%s.%s", wc.WhatsAppCampaignDispatchTopic, input.CampaignID)
	published := 0
	publishErr := uc.publishEntries(topic, toPublish, &published)
	if publishErr != nil {

		missing := int64(len(toPublish) - published)
		if uc.shared != nil && missing > 0 {
			if _, err := uc.shared.DecrBy(remainingCounterKey(input.CampaignID), missing); err != nil {
				log.Printf("[whatsapp_campaign quick_send] failed to compensate counter for %s: %v", input.CampaignID, err)
			}
		}

		if statusTransitioned && published == 0 {
			uc.revertStatus(input.CampaignID, previousStatus)
		}
		return nil, publishErr
	}

	output.DispatchedCount = published
	output.Status = string(campaign.Status)
	return output, nil
}

func (uc *quickSendUseCase) insertEntries(workspaceID string, input wc.QuickSendInput, output *wc.QuickSendOutput) ([]wce.WhatsAppCampaignEntry, error) {
	added := make([]wce.WhatsAppCampaignEntry, 0, len(input.PhoneNumbers))

	for _, phoneInput := range input.PhoneNumbers {
		normalized := lead.NormalizeNumber(phoneInput.Number)

		existing, _ := uc.entryRepo.FindByCampaignAndNumber(input.CampaignID, normalized)
		if existing != nil {
			output.DuplicatesSkipped++
			continue
		}

		l, _, err := uc.leadRepo.FindOrCreate(workspaceID, normalized, lead.LeadUpdate{Name: phoneInput.Name})
		if err != nil {
			return nil, err
		}

		entry := &wce.WhatsAppCampaignEntry{
			ID:         uuid.New().String(),
			CampaignID: input.CampaignID,
			LeadID:     l.ID,
			Status:     wce.SendStatusPending,
			Variables:  phoneInput.Variables,
			Metadata:   phoneInput.Metadata,
		}
		if err := uc.entryRepo.Create(entry); err != nil {
			return nil, err
		}
		entry.Lead = l

		added = append(added, *entry)
		output.AddedCount++
		output.Entries = append(output.Entries, wc.EntryOutput{
			EntryID:    entry.ID,
			CampaignID: entry.CampaignID,
			LeadID:     l.ID,
			Number:     l.Number,
			Name:       l.Name,
			Variables:  entry.Variables,
			Status:     string(entry.Status),
			Metadata:   phoneInput.Metadata,
			CreatedAt:  entry.CreatedAt.Format(time.RFC3339),
			UpdatedAt:  entry.UpdatedAt.Format(time.RFC3339),
		})
	}

	return added, nil
}

func (uc *quickSendUseCase) ensureConsumer(campaignID string, previousStatus wc.Status) error {
	if uc.messageConsumerUC == nil {
		return nil
	}
	switch {
	case !uc.messageConsumerUC.IsSubscribed(campaignID):
		if err := uc.messageConsumerUC.SubscribeToCampaign(campaignID); err != nil {
			return fmt.Errorf("failed to subscribe to campaign: %w", err)
		}
	case previousStatus == wc.CampaignStatusPaused:
		if err := uc.messageConsumerUC.ResumeCampaignConsumer(campaignID); err != nil {
			return fmt.Errorf("failed to resume campaign consumer: %w", err)
		}
	}
	return nil
}

func (uc *quickSendUseCase) publishEntries(topic string, entries []wce.WhatsAppCampaignEntry, published *int) error {
	for _, entry := range entries {
		number := ""
		if entry.Lead != nil {
			number = entry.Lead.Number
		}
		if number == "" {

			log.Printf("[whatsapp_campaign quick_send] skipping entry %s without phone number", entry.ID)
			continue
		}

		payload, err := json.Marshal(wc.DispatchQueueMessage{
			CampaignID:  entry.CampaignID,
			EntryID:     entry.ID,
			PhoneNumber: number,
		})
		if err != nil {
			return fmt.Errorf("failed to marshal whatsapp campaign dispatch payload: %w", err)
		}

		if err := uc.messageQueuePub.Publish(topic, payload); err != nil {
			return fmt.Errorf("failed to publish whatsapp campaign dispatch payload: %w", err)
		}
		*published++
	}
	return nil
}

func (uc *quickSendUseCase) revertStatus(campaignID string, previous wc.Status) {
	if previous == "" || previous == wc.CampaignStatusRunning {
		return
	}
	if _, err := uc.campaignRepo.UpdateStatus(campaignID, previous, wc.CampaignStatusRunning); err != nil {
		log.Printf("[whatsapp_campaign quick_send] failed to revert campaign %s status to %s: %v", campaignID, previous, err)
	}
}
