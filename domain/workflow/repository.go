package workflow

import "vozko/domain/shared"

type WorkflowRepository interface {
	Create(workflow *Workflow) error
	Update(workflow *Workflow) error
	Delete(workflowID string) error
	FindByID(workflowID string) (*Workflow, error)
	FindByWorkspaceID(workspaceID string) ([]*Workflow, error)
	List(input ListWorkflowsInput) (*shared.PaginatedResult[*Workflow], error)
	FindActiveByTrigger(workspaceID string, triggerType TriggerType) ([]*Workflow, error)
}

type WorkflowRunRepository interface {
	Create(run *WorkflowRun) error
	Update(run *WorkflowRun) error
	FindByID(runID string) (*WorkflowRun, error)
	FindActiveByEntry(workflowID, entryID string) (*WorkflowRun, error)
	FindActiveByEntryAndTrigger(workflowID, entryID, triggerNodeID string) (*WorkflowRun, error)
	// FindWaitingReplyByEntry finds a run parked at a wait-for-reply node for the
	// given entry, regardless of its workflow or start trigger, so an incoming
	// message can resume it even when the workflow isn't message_received-triggered.
	FindWaitingReplyByEntry(entryID string) (*WorkflowRun, error)
	List(input ListRunsInput) (*shared.PaginatedResult[*WorkflowRun], error)
	FindWakeableRuns(now int64, limit int) ([]*WorkflowRun, error)
	FindStuckRuns(cutoff int64) ([]*WorkflowRun, error)
	CancelByWorkflow(workflowID string) (int64, error)
	CountByWorkflow(workflowID string) (int64, error)
	CountActiveByWorkspace(workspaceID string) (int64, error)
}

type WorkflowRunLogRepository interface {
	Create(log *WorkflowRunLog) error
	FindByRunID(runID string) ([]*WorkflowRunLog, error)
	CountByRun(runID string) (int64, error)
}
