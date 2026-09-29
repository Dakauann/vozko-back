package aibilling

import (
	"encoding/json"
	"log"
	"math"
	"time"

	"github.com/google/uuid"

	"vozko/domain/ai"
	"vozko/domain/messaging"
)

type Publisher struct {
	queue  messaging.MessageQueuePub
	delays []time.Duration
}

func NewPublisher(queue messaging.MessageQueuePub) *Publisher {
	return &Publisher{
		queue:  queue,
		delays: []time.Duration{100 * time.Millisecond, 500 * time.Millisecond, 2 * time.Second},
	}
}

func (p *Publisher) Publish(workspaceID, model string, promptTokens, completionTokens int, providerCostMicros int64) {
	if p == nil || p.queue == nil {
		return
	}
	data, err := json.Marshal(ai.AICompletedEvent{
		RequestID:          uuid.New().String(),
		WorkspaceID:        workspaceID,
		Model:              model,
		PromptTokens:       promptTokens,
		CompletionTokens:   completionTokens,
		ProviderCostMicros: providerCostMicros,
	})
	if err != nil {
		log.Printf("[ai-billing] failed to marshal event: %v", err)
		return
	}
	for attempt, delay := range p.delays {
		err := p.queue.Publish(ai.TopicAIBillingCompleted, data)
		if err == nil {
			return
		}
		if attempt == len(p.delays)-1 {
			log.Printf("[ai-billing] CRITICAL publish failed after %d attempts workspace=%s model=%s prompt=%d completion=%d: %v",
				len(p.delays), workspaceID, model, promptTokens, completionTokens, err)
			return
		}
		log.Printf("[ai-billing] publish attempt %d failed, retrying in %v: %v", attempt+1, delay, err)
		time.Sleep(delay)
	}
}

func CostToMicros(cost float64) int64 {
	if cost <= 0 {
		return 0
	}
	return int64(math.Ceil(cost * 1_000_000))
}
