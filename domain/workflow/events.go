package workflow

const (
	EventWorkflowCreated   = "workflow:created"
	EventWorkflowUpdated   = "workflow:updated"
	EventWorkflowDeleted   = "workflow:deleted"
	EventWorkflowActivated = "workflow:activated"
	EventWorkflowPaused    = "workflow:paused"

	EventRunStarted   = "workflow_run:started"
	EventRunCompleted = "workflow_run:completed"
	EventRunError     = "workflow_run:error"
	EventRunCancelled = "workflow_run:cancelled"
	EventRunWaiting   = "workflow_run:waiting"
	EventRunResumed   = "workflow_run:resumed"

	EventNodeExecuted = "workflow_node:executed"
	EventNodeFailed   = "workflow_node:failed"
)

type WorkflowEvent struct {
	Type        string      `json:"type"`
	WorkspaceID string      `json:"workspaceId"`
	WorkflowID  string      `json:"workflowId,omitempty"`
	RunID       string      `json:"runId,omitempty"`
	EntryID     string      `json:"entryId,omitempty"`
	NodeID      string      `json:"nodeId,omitempty"`
	Data        interface{} `json:"data,omitempty"`
}
