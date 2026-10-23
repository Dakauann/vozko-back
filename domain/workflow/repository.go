package workflow

import (
	"context"
	"time"

	"vozko/domain/shared"
)

type ConversationRunCanceller interface {
	CancelConversationRuns(ctx context.Context, workspaceID, entryID, entryType, reason string) ([]CancelledRun, error)
}

type WorkflowRepository interface {
	Create(workflow *Workflow) error
	SaveDraft(workflow *Workflow, expectedRevision int) (int, error)
	UpdateDepartment(workflowID, departmentID string) error
	Delete(workflowID string) error
	FindByID(workflowID string) (*Workflow, error)
	FindByWorkspaceID(workspaceID string) ([]*Workflow, error)
	List(input ListWorkflowsInput) (*shared.PaginatedResult[*Workflow], error)
	FindActiveByTrigger(workspaceID string, triggerType TriggerType) ([]*Workflow, error)
	UpdateStatus(workflowID string, status WorkflowStatus) error
	PublishVersion(in PublishVersion) (*WorkflowVersion, error)
	FindVersion(workflowID string, version int) (*WorkflowVersion, error)
	ListVersions(workflowID string, options shared.QueryOptions) (*shared.PaginatedResult[*WorkflowVersion], error)
}

type WorkflowRunRepository interface {
	Create(run *WorkflowRun) error
	Update(run *WorkflowRun) error
	FindByID(runID string) (*WorkflowRun, error)
	FindActiveByEntry(workflowID, entryID string) (*WorkflowRun, error)
	FindActiveByEntryAndTrigger(workflowID, entryID, triggerNodeID string) (*WorkflowRun, error)
	FindWaitingReplyByEntry(entryID string) (*WorkflowRun, error)
	List(input ListRunsInput) (*shared.PaginatedResult[*WorkflowRun], error)
	FindWakeableRuns(now int64, limit int) ([]*WorkflowRun, error)
	FindStuckRuns(cutoff int64) ([]*WorkflowRun, error)
	CancelByWorkflow(workflowID string) (int64, error)
	CountByWorkflow(workflowID string) (int64, error)
	CountActiveByWorkspace(workspaceID string) (int64, error)
}

const CancelReasonOperator = "cancelled_by_operator"

type RunCanceller interface {
	CancelRun(runID, reason string) (bool, error)
}

type RunStatusReader interface {
	RunStatus(runID string) (RunStatus, error)
}

type WorkflowRunLogRepository interface {
	Create(log *WorkflowRunLog) error
	FindByRunID(runID string) ([]*WorkflowRunLog, error)
	PageByRunID(runID string, offset, limit int) ([]*WorkflowRunLog, error)
	CountByRun(runID string) (int64, error)
}

type TraceRetentionRepository interface {
	PruneStepPayloads(executedBefore time.Time, limit int) (int64, error)
	PruneTriggerData(startedBefore time.Time, limit int) (int64, error)
	DeleteStepsOfFinishedRuns(startedBefore time.Time, limit int) (int64, error)
	DeleteFinishedRuns(startedBefore time.Time, limit int) (int64, error)
	PruneVersions(keepLatest, limit int) (int64, error)
}
