package workflow_usecase

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"vozko/domain/workflow"
)

var (
	errAgentCallStatusIncomplete   = errors.New("workflow: the agent run status needs the agent run read model and the step names")
	errAgentRunCancellerIncomplete = errors.New("workflow: cancelling agent runs on finish needs the agent runs and the step log")
)

type StepNamer = workflow.StepNamer

type AgentCallStatus struct {
	runs  workflow.AgentCallRuns
	steps StepNamer
	now   func() time.Time
}

func NewAgentCallStatus(runs workflow.AgentCallRuns, steps StepNamer) (*AgentCallStatus, error) {
	if runs == nil || steps == nil {
		return nil, errAgentCallStatusIncomplete
	}
	return &AgentCallStatus{runs: runs, steps: steps, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *AgentCallStatus) ContextLine(workspaceID, entryID, entryType string) string {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(entryID) == "" || strings.TrimSpace(entryType) == "" {
		return ""
	}
	run, err := s.runs.LatestForEntry(workspaceID, entryID, entryType)
	if err != nil {
		slog.Warn("workflow.agent_run.status_unreadable",
			slog.String("workspace_id", workspaceID), slog.String("entry_id", entryID), slog.String("error", err.Error()))
		return ""
	}
	if run == nil {
		return ""
	}
	run.CurrentStepName = s.steps.StepName(run.CurrentNodeType)
	return workflow.AgentCallContextLine(run, s.now())
}

type AgentRunFinishCanceller struct {
	runs workflow.AgentRunCanceller
	logs workflow.WorkflowRunLogRepository
}

func NewAgentRunFinishCanceller(runs workflow.AgentRunCanceller, logs workflow.WorkflowRunLogRepository) (*AgentRunFinishCanceller, error) {
	if runs == nil || logs == nil {
		return nil, errAgentRunCancellerIncomplete
	}
	return &AgentRunFinishCanceller{runs: runs, logs: logs}, nil
}

func (c *AgentRunFinishCanceller) CancelAgentRunsOfFinishedConversation(entryID, entryType string) error {
	cancelled, err := c.runs.CancelActiveAgentRuns(entryID, entryType, workflow.CancelReasonConversationFinished)
	if err != nil {
		return fmt.Errorf("workflow: cancelling the agent runs of %s %s: %w", entryType, entryID, err)
	}
	for _, run := range cancelled {
		c.traceCancel(run)
	}
	return nil
}

func (c *AgentRunFinishCanceller) traceCancel(run workflow.CancelledAgentRun) {
	attrs := []any{
		slog.String("run_id", run.RunID), slog.String("node_id", run.CurrentNodeID),
		slog.String("workspace_id", run.WorkspaceID), slog.String("workflow_id", run.WorkflowID),
	}
	traceCancelledStep(c.logs, run.RunID, run.CurrentNodeID, workflow.CancelReasonConversationFinished, attrs)
}
