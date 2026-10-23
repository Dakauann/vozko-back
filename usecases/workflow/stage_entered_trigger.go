package workflow_usecase

import (
	"encoding/json"
	"errors"
	"log/slog"

	"vozko/domain/messaging"
	"vozko/domain/stage"
	"vozko/domain/workflow"
	"vozko/usecases/shared/queueworkers"
)

var (
	errNoStageQueue            = errors.New("stage trigger: no queue publisher wired")
	errStageConsumerIncomplete = errors.New("stage trigger: consumer needs a queue subscriber, a queue publisher and a trigger evaluator")
)

type stageEnteredMessage struct {
	Entered stage.Entered `json:"entered"`
	Attempt int           `json:"attempt,omitempty"`
}

func enteredLogAttrs(e stage.Entered) []any {
	return []any{
		slog.String("workspace_id", e.WorkspaceID),
		slog.String("entry_id", e.EntryID),
		slog.String("stage_id", e.StageID),
		slog.String("move_id", e.MoveID),
		slog.String("by_workflow_id", e.Cause.WorkflowID),
		slog.String("by_run_id", e.Cause.RunID),
		slog.Int("chain", e.Cause.Chain),
	}
}

type StageEnteredPublisher struct {
	pub messaging.MessageQueuePub
}

func NewStageEnteredPublisher(pub messaging.MessageQueuePub) *StageEnteredPublisher {
	return &StageEnteredPublisher{pub: pub}
}

func (p *StageEnteredPublisher) StageEntered(e stage.Entered) {
	if err := p.publish(e); err != nil {
		slog.Error("workflow.stage_trigger.publish_failed", append(enteredLogAttrs(e), slog.String("error", err.Error()))...)
	}
}

func (p *StageEnteredPublisher) publish(e stage.Entered) error {
	if p == nil || p.pub == nil {
		return errNoStageQueue
	}
	payload, err := json.Marshal(stageEnteredMessage{Entered: e})
	if err != nil {
		return err
	}
	return p.pub.Publish(workflow.TopicStageEntered, payload)
}

type ConsumeStageEnteredUseCase struct {
	sub       messaging.MessageQueueSub
	pub       messaging.MessageQueuePub
	evaluator workflow.CheckedTriggerEvaluator
	workers   *queueworkers.Pool
}

func NewConsumeStageEnteredUseCase(sub messaging.MessageQueueSub, pub messaging.MessageQueuePub, evaluator workflow.CheckedTriggerEvaluator) *ConsumeStageEnteredUseCase {
	return &ConsumeStageEnteredUseCase{sub: sub, pub: pub, evaluator: evaluator, workers: queueworkers.NewPool(workflow.StageEnteredWorkers)}
}

func (c *ConsumeStageEnteredUseCase) Start() error {
	if c == nil || c.sub == nil || c.pub == nil || c.evaluator == nil {
		return errStageConsumerIncomplete
	}
	return c.sub.Subscribe(workflow.TopicStageEntered, c.workers.Handler(c.handle))
}

func (c *ConsumeStageEnteredUseCase) handle(payload []byte, ack messaging.MessageAck) {
	var msg stageEnteredMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		slog.Error("workflow.stage_trigger.unreadable_message", slog.String("error", err.Error()))
		_ = ack.Nack(false)
		return
	}
	e := msg.Entered
	event, starts := workflow.StageAddedEvent(workflow.StageEntry{
		MoveID:       e.MoveID,
		WorkspaceID:  e.WorkspaceID,
		EntryID:      e.EntryID,
		EntryType:    e.EntryType,
		StageID:      e.StageID,
		ByWorkflowID: e.Cause.WorkflowID,
		ByChain:      e.Cause.Chain,
	})
	if !starts {
		slog.Info("workflow.stage_trigger.not_started", enteredLogAttrs(e)...)
		_ = ack.Ack()
		return
	}
	if err := c.evaluator.EvaluateChecked(event); err != nil {
		c.retryOrDrop(msg, ack, err)
		return
	}
	_ = ack.Ack()
}

func (c *ConsumeStageEnteredUseCase) retryOrDrop(msg stageEnteredMessage, ack messaging.MessageAck, cause error) {
	attempt := msg.Attempt + 1
	attrs := append(enteredLogAttrs(msg.Entered), slog.Int("attempt", attempt), slog.Int("max_attempts", messaging.MaxRetries), slog.String("error", cause.Error()))
	if attempt >= messaging.MaxRetries {
		slog.Error("workflow.stage_trigger.dropped", attrs...)
		_ = ack.Ack()
		return
	}
	msg.Attempt = attempt
	payload, err := json.Marshal(msg)
	if err == nil {
		err = c.pub.PublishWithDelay(workflow.TopicStageEntered, payload, retryBackoff(attempt))
	}
	if err != nil {
		slog.Error("workflow.stage_trigger.retry_not_queued", append(attrs, slog.String("queue_error", err.Error()))...)
		_ = ack.Nack(true)
		return
	}
	slog.Warn("workflow.stage_trigger.retry_scheduled", append(attrs, slog.Duration("delay", retryBackoff(attempt)))...)
	_ = ack.Ack()
}
