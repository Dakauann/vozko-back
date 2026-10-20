package ai_attendance

import (
	"time"

	"vozko/domain/shared"
)

type ToolCallOutcome string

const (
	ToolCallOK        ToolCallOutcome = "ok"
	ToolCallError     ToolCallOutcome = "error"
	ToolCallRequested ToolCallOutcome = "requested"
)

type ToolCall struct {
	ID          string                 `json:"id"`
	ToolCallID  string                 `json:"toolCallId,omitempty"`
	WorkspaceID string                 `json:"workspaceId"`
	EntryID     string                 `json:"entryId"`
	EntryType   string                 `json:"entryType"`
	AgentID     string                 `json:"agentId"`
	SessionID   string                 `json:"sessionId,omitempty"`
	ToolName    string                 `json:"toolName"`
	Args        map[string]interface{} `json:"args,omitempty"`
	Outcome     ToolCallOutcome        `json:"outcome"`
	Error       string                 `json:"error,omitempty"`
	ErrorCode   string                 `json:"errorCode,omitempty"`
	DurationMs  int64                  `json:"durationMs"`
	RunID       string                 `json:"runId,omitempty"`
	WorkflowID  string                 `json:"workflowId,omitempty"`
	CreatedAt   time.Time              `json:"createdAt"`
}

type ToolCallLog interface {
	Record(calls []ToolCall) error
	ListByEntry(workspaceID, entryID, entryType string, pagination shared.Pagination) (*shared.PaginatedResult[*ToolCall], error)
	DeleteCreatedBefore(cutoff time.Time, limit int) (int64, error)
}
