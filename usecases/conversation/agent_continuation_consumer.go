package conversation_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"vozko/domain/messaging"
	"vozko/domain/workflow"
)

const (
	agentContinuationTimeout     = 3 * time.Minute
	agentContinuationConcurrency = 10
)

var errAgentContinuationQueueMissing = errors.New("agent continuation: no queue subscriber wired")

type AgentRunFinishedHandler interface {
	Handle(ctx context.Context, ev workflow.AgentRunFinished) string
}

type ConsumeAgentRunFinished struct {
	sub     messaging.MessageQueueSub
	handler AgentRunFinishedHandler
	slots   chan struct{}
}

func NewConsumeAgentRunFinished(sub messaging.MessageQueueSub, handler AgentRunFinishedHandler) *ConsumeAgentRunFinished {
	return &ConsumeAgentRunFinished{sub: sub, handler: handler, slots: make(chan struct{}, agentContinuationConcurrency)}
}

func (c *ConsumeAgentRunFinished) Start() error {
	if c == nil || c.sub == nil || c.handler == nil {
		return errAgentContinuationQueueMissing
	}
	return c.sub.Subscribe(workflow.TopicAgentRunFinished, c.handle)
}

func (c *ConsumeAgentRunFinished) handle(payload []byte, ack messaging.MessageAck) {
	var ev workflow.AgentRunFinished
	if err := json.Unmarshal(payload, &ev); err != nil || ev.RunID == "" {
		slog.Error("conversation.agent_continuation.invalid_event", slog.Any("error", err))
		_ = ack.Nack(false)
		return
	}
	c.slots <- struct{}{}
	go func() {
		defer func() { <-c.slots }()
		ctx, cancel := context.WithTimeout(context.Background(), agentContinuationTimeout)
		defer cancel()
		c.handler.Handle(ctx, ev)
		if err := ack.Ack(); err != nil {
			slog.Warn("conversation.agent_continuation.ack_failed", slog.String("run_id", ev.RunID), slog.String("error", err.Error()))
		}
	}()
}
