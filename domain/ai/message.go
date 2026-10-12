package ai

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ReasoningKind string

const (
	ReasoningText      ReasoningKind = "text"
	ReasoningSummary   ReasoningKind = "summary"
	ReasoningEncrypted ReasoningKind = "encrypted"
)

type ReasoningBlock struct {
	ID        string
	Index     int
	Kind      ReasoningKind
	Text      string
	Summary   string
	Data      string
	Format    string
	Signature string
}

type Message struct {
	Role       Role
	Content    string
	ToolCallID string
	ToolCalls  []ToolCall
	Images     []string
	Reasoning  []ReasoningBlock
}
