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

// Trigger-data keys the interactive prompt node branches on.
//
// They are named constants rather than string literals at each call site
// because they are a contract between three channel handlers and
// AdvanceOnReply. A typo in any one channel does not fail loudly, it silently
// routes every button press down the no_match branch, which reads as "the
// customer typed something unexpected" and is very hard to trace back.
const (
	DataKeySelectedOptionID    = "selected_option_id"
	DataKeySelectedOptionTitle = "selected_option_title"
	DataKeySelectedOptionKind  = "selected_option_type"
)

// OptionSelection is the option a contact tapped, when the inbound event was a
// tap on an interactive prompt rather than typed text.
//
// Every channel reports one differently, WhatsApp nests it under
// interactive.button_reply/list_reply, Telegram sends a callback_query,
// Instagram sets message.quick_reply.payload, and all three normalize to this.
type OptionSelection struct {
	// ID is the payload the option was sent with. It is what the workflow
	// branches on, and it must survive the round trip byte-for-byte.
	ID string
	// Title is the label the contact saw. Display only: an author may reword it
	// between the send and the reply.
	Title string
	// Kind names the provider mechanism, for observability only.
	Kind string
}

// ApplySelection writes the tapped-option keys into a trigger's data map.
//
// A nil selection writes nothing, so a channel can call this unconditionally
// for both typed replies and taps.
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
