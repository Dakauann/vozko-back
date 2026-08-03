package aichat

import "time"

// Role identifies who produced a chat message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool"
)

// Thread is a single in-app AI chat conversation, owned by a user and scoped to a
// workspace (the billing + access-control boundary).
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

// Message is one turn within a Thread. ToolCalls holds the raw JSON of any tool
// calls/results on an assistant turn. Token counts are for display/audit only,
// billing is handled out-of-band via the ai.billing.completed event.
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
