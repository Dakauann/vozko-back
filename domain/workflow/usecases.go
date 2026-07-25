package workflow

import (
	"context"
	"vozko/domain/shared"
)

type CreateWorkflowUseCase interface {
	Execute(ctx context.Context, workflow *Workflow) (*Workflow, error)
}

type UpdateWorkflowUseCase interface {
	Execute(workflowID string, workflow *Workflow) (*Workflow, error)
}

type AssignDepartmentUseCase interface {
	Execute(ctx context.Context, workflowID string) (*Workflow, error)
}

type DeleteWorkflowUseCase interface {
	Execute(workflowID string) error
}

type GetWorkflowUseCase interface {
	Execute(workflowID string) (*Workflow, error)
}

type ListWorkflowsUseCase interface {
	Execute(input ListWorkflowsInput) (*shared.PaginatedResult[*Workflow], error)
}

type ActivateWorkflowUseCase interface {
	Execute(workflowID string) (*Workflow, error)
}

type PauseWorkflowUseCase interface {
	Execute(workflowID string) (*Workflow, error)
}

type StartRunUseCase interface {
	Execute(input StartRunInput) (*WorkflowRun, error)
}

type StartRunInput struct {
	WorkflowID  string
	WorkspaceID string
	EntryID     string
	EntryType   string
	Variables   map[string]interface{}
}

type ResumeRunUseCase interface {
	Execute(input ResumeRunInput) (*WorkflowRun, error)
}

type ResumeRunInput struct {
	RunID     string
	ReplyText string
	EventName string
	EventData map[string]interface{}
}

type CancelRunUseCase interface {
	Execute(runID string) error
}

type GetRunUseCase interface {
	Execute(runID string) (*RunDetail, error)
}

type RunDetail struct {
	Run  *WorkflowRun      `json:"run"`
	Logs []*WorkflowRunLog `json:"logs"`
}

type ListRunsUseCase interface {
	Execute(input ListRunsInput) (*shared.PaginatedResult[*WorkflowRun], error)
}

type NodeExecutor interface {
	Execute(ctx *NodeContext) (*NodeResult, error)
	Definition() NodeDefinition
}

type NodeContext struct {
	Run      *WorkflowRun
	Node     *Node
	Graph    *Graph
	Workflow *Workflow
	State    *RunState
	Runtime  interface{}
}

type NodeResult struct {
	NextNodeID string
	Output     map[string]interface{}
	Wait       *WaitInstruction
	Error      string
	Complete   bool
}

type WaitInstruction struct {
	WakeAt int64
	Reason WaitReason
}

type WorkflowManager interface {
	Tick()
}

type TriggerEvaluator interface {
	Evaluate(event TriggerEvent)
}

type TriggerEvent struct {
	WorkspaceID string
	EntryID     string
	EntryType   string
	TriggerType TriggerType
	Data        map[string]interface{}
}

type WorkflowDashboardUseCase interface {
	Execute(workspaceID string) (*DashboardStats, error)
}

type DashboardStats struct {
	TotalWorkflows  int64 `json:"totalWorkflows"`
	ActiveWorkflows int64 `json:"activeWorkflows"`
	TotalRuns       int64 `json:"totalRuns"`
	ActiveRuns      int64 `json:"activeRuns"`
	CompletedRuns   int64 `json:"completedRuns"`
	ErrorRuns       int64 `json:"errorRuns"`
}
