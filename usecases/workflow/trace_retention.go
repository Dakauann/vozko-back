package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	aa "vozko/domain/ai_attendance"
	"vozko/domain/workflow"
)

const (
	StepPayloadRetention     = 30 * 24 * time.Hour
	RunRetention             = 180 * 24 * time.Hour
	WebhookDeliveryRetention = 30 * 24 * time.Hour
	ToolCallRetention        = 180 * 24 * time.Hour

	retentionBatchSize  = 5000
	retentionMaxBatches = 200
)

type ResolvedAlertPruner interface {
	DeleteResolvedBefore(cutoff time.Time, limit int) (int64, error)
}

type TraceRetention struct {
	traces     workflow.TraceRetentionRepository
	deliveries workflow.WebhookDeliveryRepository
	toolCalls  aa.ToolCallLog
	alerts     ResolvedAlertPruner
	now        func() time.Time
}

func NewTraceRetention(traces workflow.TraceRetentionRepository, deliveries workflow.WebhookDeliveryRepository, toolCalls aa.ToolCallLog, alerts ResolvedAlertPruner) *TraceRetention {
	if traces == nil || deliveries == nil || toolCalls == nil || alerts == nil {
		panic("workflow: trace retention needs the step, delivery, tool call and runtime alert stores")
	}
	return &TraceRetention{traces: traces, deliveries: deliveries, toolCalls: toolCalls, alerts: alerts, now: func() time.Time { return time.Now().UTC() }}
}

func (j *TraceRetention) pruneVersions(_ time.Time, limit int) (int64, error) {
	return j.traces.PruneVersions(workflow.VersionsKeptPerWorkflow, limit)
}

type retentionPass struct {
	name      string
	before    time.Time
	run       func(before time.Time, limit int) (int64, error)
	dependsOn string
}

func (j *TraceRetention) Execute(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := j.now()
	passes := []retentionPass{
		{name: "step_payloads", before: now.Add(-StepPayloadRetention), run: j.traces.PruneStepPayloads},
		{name: "run_trigger_data", before: now.Add(-StepPayloadRetention), run: j.traces.PruneTriggerData},
		{name: "run_steps", before: now.Add(-RunRetention), run: j.traces.DeleteStepsOfFinishedRuns},
		{name: "runs", before: now.Add(-RunRetention), run: j.traces.DeleteFinishedRuns, dependsOn: "run_steps"},
		{name: "webhook_deliveries", before: now.Add(-WebhookDeliveryRetention), run: j.deliveries.DeleteReceivedBefore},
		{name: "ai_tool_calls", before: now.Add(-ToolCallRetention), run: j.toolCalls.DeleteCreatedBefore},
		{name: "runtime_alerts", before: now.Add(-workflow.RuntimeAlertRetention), run: j.alerts.DeleteResolvedBefore},
		{name: "workflow_versions", run: j.pruneVersions},
	}
	failed := map[string]bool{}
	var errs []error
	for _, pass := range passes {
		if pass.dependsOn != "" && failed[pass.dependsOn] {
			continue
		}
		affected, err := drainInBatches(ctx, pass)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if err != nil {
			failed[pass.name] = true
			errs = append(errs, fmt.Errorf("%s: %w", pass.name, err))
			slog.Error("workflow.retention.pass_failed", slog.String("pass", pass.name), slog.Int64("affected", affected), slog.String("error", err.Error()))
			continue
		}
		if affected > 0 {
			slog.Info("workflow.retention.pass_done", slog.String("pass", pass.name), slog.Int64("affected", affected), slog.Time("before", pass.before))
		}
	}
	return errors.Join(errs...)
}

func drainInBatches(ctx context.Context, pass retentionPass) (int64, error) {
	var total int64
	for batch := 0; batch < retentionMaxBatches; batch++ {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		affected, err := pass.run(pass.before, retentionBatchSize)
		total += affected
		if err != nil {
			return total, err
		}
		if affected < retentionBatchSize {
			return total, nil
		}
	}
	return total, nil
}
