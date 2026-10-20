package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"vozko/domain/workflow"
)

type AgentContinuationSweep struct {
	backlog   workflow.AgentContinuationBacklog
	publisher workflow.AgentRunFinishedPublisher
	now       func() time.Time
}

func NewAgentContinuationSweep(backlog workflow.AgentContinuationBacklog, publisher workflow.AgentRunFinishedPublisher) *AgentContinuationSweep {
	if backlog == nil || publisher == nil {
		panic("workflow: the continuation sweep needs the run backlog and the event publisher")
	}
	return &AgentContinuationSweep{backlog: backlog, publisher: publisher, now: func() time.Time { return time.Now().UTC() }}
}

func (s *AgentContinuationSweep) Execute(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := s.now()
	runs, err := s.backlog.FinishedWithoutContinuation(now.Add(-workflow.ContinuationSweepWindow), now.Add(-workflow.ContinuationSweepGrace), workflow.ContinuationSweepLimit)
	if err != nil {
		return fmt.Errorf("read agent runs awaiting a continuation: %w", err)
	}
	var errs []error
	requeued := 0
	for _, run := range runs {
		event, ok := workflow.AgentRunFinishedOf(run)
		if !ok || event.Continuation() == workflow.ContinuationNone {
			continue
		}
		if err := s.publisher.PublishAgentRunFinished(event); err != nil {
			slog.Error("workflow.continuation_sweep.publish_failed", append(runLogAttrs(run, run.CurrentNodeID), slog.String("error", err.Error()))...)
			errs = append(errs, err)
			continue
		}
		requeued++
	}
	if requeued > 0 {
		slog.Info("workflow.continuation_sweep.requeued", slog.Int("runs", requeued))
	}
	return errors.Join(errs...)
}
