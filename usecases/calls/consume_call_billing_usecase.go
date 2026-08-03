package calls_usecase

import (
	"encoding/json"
	"fmt"
	"log"
	"math"

	"github.com/google/uuid"

	"vozko/domain/balance"
	"vozko/domain/calls/billing"
	cdr "vozko/domain/calls/cdr"
	"vozko/domain/messaging"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type ConsumeCallBillingUseCase struct {
	subscriber  messaging.MessageQueueSub
	billingRepo billing.Repository
	balanceRepo balance.Repository
	pricer      workspace_pricing.Pricer
	cdrComplete cdr.CompleteCallUseCase
	logger      *log.Logger
	semaphore   chan struct{}
}

func NewConsumeCallBillingUseCase(
	subscriber messaging.MessageQueueSub,
	billingRepo billing.Repository,
	balanceRepo balance.Repository,
	pricer workspace_pricing.Pricer,
	cdrComplete cdr.CompleteCallUseCase,
	logger *log.Logger,
) *ConsumeCallBillingUseCase {
	return &ConsumeCallBillingUseCase{
		subscriber:  subscriber,
		billingRepo: billingRepo,
		balanceRepo: balanceRepo,
		pricer:      pricer,
		cdrComplete: cdrComplete,
		logger:      logger,
		semaphore:   make(chan struct{}, 5),
	}
}

func (c *ConsumeCallBillingUseCase) Start() error {
	return c.subscriber.Subscribe(billing.TopicCallCompleted, func(message []byte, ack messaging.MessageAck) {
		c.handleBillingEvent(message, ack)
	})
}

func (c *ConsumeCallBillingUseCase) handleBillingEvent(message []byte, ack messaging.MessageAck) {
	var event billing.CallCompletedEvent
	if err := json.Unmarshal(message, &event); err != nil {
		c.logf("billing consumer: bad message, nacking: %v", err)
		_ = ack.Nack(false)
		return
	}

	c.semaphore <- struct{}{}
	go func() {
		defer func() { <-c.semaphore }()
		defer func() {
			if r := recover(); r != nil {
				c.logf("billing consumer: panic processing call %s: %v", event.CallID, r)
				_ = ack.Nack(false)
			}
		}()

		if err := c.processEvent(event); err != nil {
			requeue := ack.DeliveryCount() < messaging.MaxRetries
			if !requeue {
				c.logf("CRITICAL: billing consumer permanently dropping call %s after %d retries, REVENUE LOST: %v",
					event.CallID, messaging.MaxRetries, err)
				c.markChargeFailed(event.CallID)
			} else {
				c.logf("billing consumer: call %s failed (attempt %d/%d): %v",
					event.CallID, ack.DeliveryCount(), messaging.MaxRetries, err)
			}
			_ = ack.Nack(requeue)
			return
		}
		_ = ack.Ack()
	}()
}

func (c *ConsumeCallBillingUseCase) processEvent(event billing.CallCompletedEvent) error {
	existing, err := c.billingRepo.GetByCallID(event.CallID)
	if err == nil && existing != nil && existing.Status == billing.StatusCharged {
		c.logf("billing consumer: call %s already charged, skipping", event.CallID)
		return nil
	}

	telDurationSec := float64(event.DurationSec)
	if event.DurationSec <= 0 {
		telDurationSec = event.CallEnd.Sub(event.CallStart).Seconds()
	}
	telChannel := ""
	if cdr.IsWhatsAppCallID(event.CallID) {
		telChannel = workspace_pricing.TelephonyChannelWhatsApp
	}
	telResult, err := c.pricer.PriceTelephonyChannel(event.WorkspaceID, telDurationSec, telChannel)
	if err != nil {
		return fmt.Errorf("telephony pricing failed for call %s: %w", event.CallID, err)
	}

	totalPrice := telResult.PriceMicros
	debitPrice := telResult.PriceMicros

	totalProviderCost := telResult.CostMicros
	totalProfit := telResult.ProfitMicros

	c.logf("billing consumer: call %s costs, tel=%d total=%d debit=%d profit=%d micros",
		event.CallID, telResult.PriceMicros, totalPrice, debitPrice, totalProfit)

	durationSec := int(math.Ceil(event.CallEnd.Sub(event.CallStart).Seconds()))
	if event.DurationSec > 0 {
		durationSec = event.DurationSec
	}

	var recordingURL *string
	if event.RecordingURL != "" {
		recordingURL = &event.RecordingURL
	}

	var callRecordID *string
	if event.CallRecordID != "" {
		id := event.CallRecordID
		callRecordID = &id
	}

	var recordID string
	if existing != nil {
		recordID = existing.ID
	} else {
		recordID = uuid.New().String()
	}

	record := &billing.CallBillingRecord{
		ID:          recordID,
		CallID:      event.CallID,
		WorkspaceID: event.WorkspaceID,
		AgentID:     event.AgentID,
		LeadID:      event.LeadID,
		CallSource:  event.CallSource,
		CallStart:   event.CallStart,
		CallEnd:     event.CallEnd,
		DurationSec: durationSec,

		TelephonyRevenueMicros: telResult.PriceMicros,
		TotalRevenueMicros:     totalPrice,

		TelephonyCostMicros: telResult.CostMicros,
		TotalCostMicros:     totalProviderCost,

		TelephonyProfitMicros: telResult.ProfitMicros,
		TotalProfitMicros:     totalProfit,
		RecordingURL:          recordingURL,
		CallRecordID:          callRecordID,
	}

	record.Status = billing.StatusInProgress
	if err := c.billingRepo.Update(record); err != nil {
		c.logf("billing consumer: failed to persist billing record for call %s: %v", event.CallID, err)
		return fmt.Errorf("billing record save failed for call %s: %w", event.CallID, err)
	}

	debitProviderCost := telResult.CostMicros
	debitProfit := telResult.ProfitMicros
	if debitPrice > 0 {
		callID := event.CallID
		alreadyDebited, existsErr := c.balanceRepo.ExistsTransactionByReferenceID(callID)
		if existsErr != nil {
			return fmt.Errorf("idempotency check failed for call %s: %w", event.CallID, existsErr)
		}
		if alreadyDebited {
			c.logf("billing consumer: call %s already debited, skipping charge", event.CallID)
		} else {
			tx, err := c.balanceRepo.DebitBalance(balance.DebitBalanceInput{
				WorkspaceID:   event.WorkspaceID,
				Amount:        debitPrice,
				ServiceType:   balance.ServiceVoiceCall,
				ReferenceID:   &callID,
				Description:   fmt.Sprintf("Chamada %s: tel=%d total=%d µ ($%.4f)", event.CallID, telResult.PriceMicros, debitPrice, float64(debitPrice)/1_000_000),
				CostMicros:    debitProviderCost,
				ProfitMicros:  debitProfit,
				AllowNegative: true,
			})
			if err != nil {
				return fmt.Errorf("debit failed for call %s: %w", event.CallID, err)
			}
			if tx != nil {
				record.TransactionID = &tx.ID
			}
		}
	}

	record.Status = billing.StatusCharged
	if err := c.billingRepo.Update(record); err != nil {
		c.logf("billing consumer: failed to update billing record for call %s: %v", event.CallID, err)
	}

	if c.cdrComplete != nil && event.CallRecordID != "" {
		if err := c.cdrComplete.Execute(cdr.CompleteCallInput{
			CallID:  event.CallID,
			EndedAt: event.CallEnd,
			Status:  cdr.StatusCompleted,
		}); err != nil {
			c.logf("billing consumer: failed to complete CDR for call %s: %v", event.CallID, err)
		}
	}

	c.logf("billing consumer: call %s charged %d µ ($%.4f) successfully",
		event.CallID, debitPrice, float64(debitPrice)/1_000_000)
	return nil
}

func (c *ConsumeCallBillingUseCase) markChargeFailed(callID string) {
	if err := c.billingRepo.UpdateStatus(callID, billing.StatusChargeFailed); err != nil {
		c.logf("billing consumer: failed to mark call %s as charge_failed: %v", callID, err)
	}
}

func (c *ConsumeCallBillingUseCase) logf(format string, args ...interface{}) {
	if c.logger != nil {
		c.logger.Printf(format, args...)
	}
}
