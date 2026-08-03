package balance_usecase

import (
	"encoding/json"
	"fmt"
	"log"

	"vozko/domain/ai"
	"vozko/domain/balance"
	"vozko/domain/messaging"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

// BillingMetrics surfaces revenue-leak signals (events that did not debit) so they
// can be alerted on instead of staying silent. Optional, nil disables metrics.
type BillingMetrics interface {
	IncBillingSkipped(reason string)
}

type ConsumeAIBillingUseCase struct {
	subscriber  messaging.MessageQueueSub
	balanceRepo balance.Repository
	pricer      workspace_pricing.Pricer
	metrics     BillingMetrics
	semaphore   chan struct{}
}

func NewConsumeAIBillingUseCase(
	subscriber messaging.MessageQueueSub,
	balanceRepo balance.Repository,
	pricer workspace_pricing.Pricer,
	metrics BillingMetrics,
) *ConsumeAIBillingUseCase {
	return &ConsumeAIBillingUseCase{
		subscriber:  subscriber,
		balanceRepo: balanceRepo,
		pricer:      pricer,
		metrics:     metrics,
		semaphore:   make(chan struct{}, 10),
	}
}

func (c *ConsumeAIBillingUseCase) markSkipped(reason string) {
	if c.metrics != nil {
		c.metrics.IncBillingSkipped(reason)
	}
}

func (c *ConsumeAIBillingUseCase) Start() error {
	return c.subscriber.Subscribe(ai.TopicAIBillingCompleted, func(message []byte, ack messaging.MessageAck) {
		c.handle(message, ack)
	})
}

func (c *ConsumeAIBillingUseCase) handle(message []byte, ack messaging.MessageAck) {
	var event ai.AICompletedEvent
	if err := json.Unmarshal(message, &event); err != nil {
		log.Printf("[ai-billing] bad message, dropping: %v", err)
		_ = ack.Nack(false)
		return
	}

	if event.WorkspaceID == "" || event.RequestID == "" || (event.PromptTokens == 0 && event.CompletionTokens == 0) {
		_ = ack.Ack()
		return
	}

	c.semaphore <- struct{}{}
	go func() {
		defer func() { <-c.semaphore }()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[ai-billing] panic processing event %s: %v", event.RequestID, r)
				_ = ack.Nack(false)
			}
		}()

		if err := c.processEvent(event); err != nil {
			requeue := ack.DeliveryCount() < messaging.MaxRetries
			if !requeue {
				c.markSkipped("permanent_drop")
				log.Printf("CRITICAL: [ai-billing] permanently dropping event %s (ws=%s, model=%s, tokens=%d+%d) after %d retries, REVENUE LOST: %v",
					event.RequestID, event.WorkspaceID, event.Model,
					event.PromptTokens, event.CompletionTokens,
					messaging.MaxRetries, err)
			} else {
				log.Printf("[ai-billing] event %s failed (attempt %d/%d): %v",
					event.RequestID, ack.DeliveryCount(), messaging.MaxRetries, err)
			}
			_ = ack.Nack(requeue)
			return
		}
		_ = ack.Ack()
	}()
}

func (c *ConsumeAIBillingUseCase) processEvent(event ai.AICompletedEvent) error {

	exists, err := c.balanceRepo.ExistsTransactionByReferenceID(event.RequestID)
	if err != nil {
		return fmt.Errorf("idempotency check failed for %s: %w", event.RequestID, err)
	}
	if exists {
		return nil
	}

	result, err := c.pricer.PriceLLM(event.WorkspaceID, event.Model, event.PromptTokens, event.CompletionTokens)
	if err != nil {
		return fmt.Errorf("LLM pricing failed for model %s: %w", event.Model, err)
	}

	fmt.Println("[ai-billing] result:", result)

	if result.PriceMicros <= 0 {
		// Usage existed (zero-token events are dropped earlier) but priced at $0,
		// the model is missing from the price table and the live fetcher too. That
		// is free AI for the customer: make it loud instead of a silent return.
		c.markSkipped("zero_price")
		log.Printf("CRITICAL: [ai-billing] priced at $0, NOT billing (ws=%s, model=%s, tokens=%d+%d, req=%s), model likely unpriced; REVENUE LEAK",
			event.WorkspaceID, event.Model, event.PromptTokens, event.CompletionTokens, event.RequestID)
		return nil
	}

	requestID := event.RequestID
	_, err = c.balanceRepo.DebitBalance(balance.DebitBalanceInput{
		WorkspaceID:   event.WorkspaceID,
		Amount:        result.PriceMicros,
		ServiceType:   balance.ServiceAI,
		ReferenceID:   &requestID,
		Description:   fmt.Sprintf("IA %s: %d entrada + %d saída tokens = %d µ ($%.4f)", event.Model, event.PromptTokens, event.CompletionTokens, result.PriceMicros, float64(result.PriceMicros)/1_000_000),
		CostMicros:    result.CostMicros,
		ProfitMicros:  result.ProfitMicros,
		AllowNegative: true,
	})
	if err != nil {
		return fmt.Errorf("debit failed for workspace %s: %w", event.WorkspaceID, err)
	}

	return nil
}
