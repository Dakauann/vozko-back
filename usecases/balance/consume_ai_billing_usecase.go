package balance_usecase

import (
	"encoding/json"
	"fmt"
	"log"

	"vozko/domain/ai"
	"vozko/domain/aiusage"
	"vozko/domain/balance"
	"vozko/domain/messaging"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type BillingMetrics interface {
	IncBillingSkipped(reason string)
}

type ConsumeAIBillingUseCase struct {
	subscriber  messaging.MessageQueueSub
	balanceRepo balance.Repository
	pricer      workspace_pricing.Pricer
	metrics     BillingMetrics
	usage       aiusage.Recorder
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

func (c *ConsumeAIBillingUseCase) SetUsageRecorder(usage aiusage.Recorder) {
	c.usage = usage
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

	if event.WorkspaceID == "" || event.RequestID == "" ||
		(event.PromptTokens == 0 && event.CompletionTokens == 0 && event.ProviderCostMicros == 0) {
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
	billed, err := c.charge(event)
	if err != nil {
		return err
	}
	return c.recordUsage(event, billed)
}

func usageRecordOf(event ai.AICompletedEvent, billed bool) aiusage.Record {
	return aiusage.Record{
		ReferenceID: event.RequestID,
		WorkspaceID: event.WorkspaceID,
		Model:       event.Model,
		Tokens: aiusage.Tokens{
			Input:      int64(event.PromptTokens),
			Output:     int64(event.CompletionTokens),
			CacheRead:  int64(event.CachedTokens),
			CacheWrite: int64(event.CacheWriteTokens),
			Reasoning:  int64(event.ReasoningTokens),
		},
		Billed: billed,
	}
}

func (c *ConsumeAIBillingUseCase) recordUsage(event ai.AICompletedEvent, billed bool) error {
	if c.usage == nil {
		return nil
	}
	if err := c.usage.Record(usageRecordOf(event, billed)); err != nil {
		return fmt.Errorf("usage record failed for %s: %w", event.RequestID, err)
	}
	return nil
}

func (c *ConsumeAIBillingUseCase) charge(event ai.AICompletedEvent) (bool, error) {
	result, err := c.pricer.PriceLLM(event.WorkspaceID, event.Model, event.PromptTokens, event.CompletionTokens, event.ProviderCostMicros)
	if err != nil {
		return false, fmt.Errorf("LLM pricing failed for model %s: %w", event.Model, err)
	}

	if result.PriceMicros <= 0 {
		c.markSkipped("zero_price")
		log.Printf("CRITICAL: [ai-billing] priced at $0, NOT billing (ws=%s, model=%s, tokens=%d+%d, req=%s), model likely unpriced; REVENUE LEAK",
			event.WorkspaceID, event.Model, event.PromptTokens, event.CompletionTokens, event.RequestID)
		return false, nil
	}

	_, err = DebitOnce(c.balanceRepo, ReferenceCharge{
		WorkspaceID:   event.WorkspaceID,
		ReferenceID:   event.RequestID,
		ServiceType:   balance.ServiceAI,
		Price:         result,
		Description:   fmt.Sprintf("IA %s: %d entrada + %d saída tokens = %d µ ($%.4f)", event.Model, event.PromptTokens, event.CompletionTokens, result.PriceMicros, float64(result.PriceMicros)/1_000_000),
		AllowNegative: true,
	})
	if err != nil {
		return false, fmt.Errorf("debit failed for workspace %s: %w", event.WorkspaceID, err)
	}
	return true, nil
}
