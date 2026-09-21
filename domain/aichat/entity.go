package aichat

import "time"

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool"
)

type Thread struct {
	ID            string
	WorkspaceID   string
	UserID        string
	Title         string
	Model         string
	LastMessageAt *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Message struct {
	ID               string
	ThreadID         string
	Role             Role
	Content          string
	Model            string
	ToolCalls        []byte
	Reasoning        []byte
	PromptTokens     int
	CompletionTokens int
	CreatedAt        time.Time
}
