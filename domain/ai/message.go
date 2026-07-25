package ai

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role    Role
	Content string
	// ToolCallID links a RoleTool (tool-result) message back to the assistant
	// tool call that produced it.
	ToolCallID string
	// ToolCalls carries the tool calls a RoleAssistant message made. Providers
	// serialize these onto the assistant message so a multi-turn agentic loop can
	// replay its own tool history (assistant tool_calls followed by their RoleTool
	// results) instead of re-grounding from scratch each turn.
	ToolCalls []ToolCall
}
