package workflow

import (
	"context"
	"time"
)

type BuilderMessageRole string

const (
	BuilderMessageRoleUser      BuilderMessageRole = "user"
	BuilderMessageRoleAssistant BuilderMessageRole = "assistant"
	BuilderMessageRoleTool      BuilderMessageRole = "tool"
	BuilderMessageRoleSystem    BuilderMessageRole = "system"
)

type BuilderMessage struct {
	Role BuilderMessageRole `json:"role"`
	Text string             `json:"text"`
	Tool string             `json:"tool,omitempty"`
	Ok   *bool              `json:"ok,omitempty"`
	At   time.Time          `json:"at"`
}

type BuilderSession struct {
	ID           string           `json:"id"`
	WorkspaceID  string           `json:"workspaceId"`
	WorkflowID   string           `json:"workflowId,omitempty"`
	Mode         string           `json:"mode"`
	Model        string           `json:"model,omitempty"`
	Title        string           `json:"title"`
	Messages     []BuilderMessage `json:"messages,omitempty"`
	MessageCount int              `json:"messageCount"`
	Valid        bool             `json:"valid"`
	StartedAt    time.Time        `json:"startedAt"`
	EndedAt      *time.Time       `json:"endedAt,omitempty"`
}

type BuilderSessionRepository interface {
	CreateSession(ctx context.Context, session *BuilderSession) error
	UpdateSession(ctx context.Context, session *BuilderSession) error
	AppendMessage(ctx context.Context, sessionID string, message BuilderMessage) error
	ListByWorkflow(ctx context.Context, workspaceID, workflowID string) ([]*BuilderSession, error)
	ListByWorkspace(ctx context.Context, workspaceID string, limit int) ([]*BuilderSession, error)
	GetByID(ctx context.Context, workspaceID, id string) (*BuilderSession, error)
}
