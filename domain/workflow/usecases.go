package workflow

import (
	"context"
	"strings"

	"vozko/domain/shared"
)

type CreateWorkflowUseCase interface {
	Execute(ctx context.Context, workflow *Workflow) (*Workflow, error)
}

type UpdateWorkflowUseCase interface {
	Execute(ctx context.Context, workflowID string, workflow *Workflow, revision int) (*Workflow, error)
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
	Execute(ctx context.Context, workflowID string) (*Workflow, error)
}

type PauseWorkflowUseCase interface {
	Execute(workflowID string) (*Workflow, error)
}

type PublishWorkflowUseCase interface {
	Publish(ctx context.Context, workflowID string) (*Workflow, error)
}

type WorkflowVersionsUseCase interface {
	List(workflowID string, options shared.QueryOptions) (*shared.PaginatedResult[*WorkflowVersion], error)
	Find(workflowID string, version int) (*WorkflowVersion, error)
}

type StartRunUseCase interface {
	Execute(input StartRunInput) (*WorkflowRun, error)
}

type StartRunInput struct {
	WorkflowID  string
	WorkspaceID string
	EntryID     string
	EntryType   string
	StartedBy   string
	Variables   map[string]interface{}
	Viewer      shared.Person
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
	Execute(viewer shared.Person, workspaceID, runID string) error
}

type GetRunUseCase interface {
	Execute(viewer shared.Person, workspaceID, runID string) (*RunDetail, error)
	RunStepPicks
}

type RunDetail struct {
	Run   *WorkflowRun      `json:"run"`
	Logs  []*WorkflowRunLog `json:"logs"`
	Steps *RunStepsPage     `json:"steps,omitempty"`
}

const (
	DefaultRunStepsPage = 500
	MaxRunStepsPage     = 1000
)

type RunStepsPage struct {
	Offset  int   `json:"offset"`
	Limit   int   `json:"limit"`
	Total   int64 `json:"total"`
	HasMore bool  `json:"hasMore"`
}

type StepWindow struct {
	Offset int
	Limit  int
}

func (w StepWindow) Normalized() StepWindow {
	if w.Offset < 0 {
		w.Offset = 0
	}
	if w.Limit <= 0 {
		w.Limit = DefaultRunStepsPage
	}
	if w.Limit > MaxRunStepsPage {
		w.Limit = MaxRunStepsPage
	}
	return w
}

type RunDetailPages interface {
	ExecutePage(viewer shared.Person, workspaceID, runID string, window StepWindow) (*RunDetail, error)
}

type ListRunsUseCase interface {
	Execute(viewer shared.Person, input ListRunsInput) (*shared.PaginatedResult[*WorkflowRun], error)
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
	Trace    *StepTrace
}

type NodeResult struct {
	NextNodeID string
	Output     map[string]interface{}
	Wait       *WaitInstruction
	Error      string
	ErrorKind  ErrorKind
	Complete   bool
	Skipped    bool
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

type ReplyWaiters interface {
	WaitingForReply(event TriggerEvent) (*WorkflowRun, error)
	WakeForReply(run *WorkflowRun, event TriggerEvent) bool
}

type TriggerEvent struct {
	WorkspaceID string
	EntryID     string
	EntryType   string
	TriggerType TriggerType
	StartedBy   string
	Data        map[string]interface{}
	Reply       *ReplyRouting
}

func (e TriggerEvent) Starter() string {
	if e.StartedBy != "" {
		return e.StartedBy
	}
	return StartedByTrigger(e.TriggerType)
}

const (
	DataKeySelectedOptionID    = "selected_option_id"
	DataKeySelectedOptionTitle = "selected_option_title"
	DataKeySelectedOptionKind  = "selected_option_type"

	DataKeyContactNumber = "contact_number"
)

type OptionSelection struct {
	ID    string
	Title string
	Kind  string
}

func ApplySelection(data map[string]interface{}, sel *OptionSelection) {
	if data == nil || sel == nil || sel.ID == "" {
		return
	}
	data[DataKeySelectedOptionID] = sel.ID
	if sel.Title != "" {
		data[DataKeySelectedOptionTitle] = sel.Title
	}
	if sel.Kind != "" {
		data[DataKeySelectedOptionKind] = sel.Kind
	}
}

const (
	DataKeyMessage     = "message"
	DataKeyMessageType = "message_type"
	DataKeyMediaType   = "media_type"
)

func InboundMessageData(text, messageType, mediaType string, sel *OptionSelection) map[string]interface{} {
	data := map[string]interface{}{DataKeyMessage: text}
	if messageType != "" {
		data[DataKeyMessageType] = messageType
	}
	if mediaType != "" {
		data[DataKeyMediaType] = mediaType
	}
	ApplySelection(data, sel)
	return data
}

func ApplyContactNumber(data map[string]interface{}, number string) {
	number = strings.TrimSpace(number)
	if data == nil || number == "" {
		return
	}
	data[DataKeyContactNumber] = number
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
