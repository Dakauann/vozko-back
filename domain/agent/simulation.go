package agent

import (
	"context"
	"errors"
)

// Agent simulation: an operator plays the lead against a real agent in a
// sandbox. The turn is assembled by the SAME recipe production uses (prompt,
// tools, knowledge base, lead memories), and the model genuinely runs, but
// every tool execution is intercepted and answered with a canned result, so
// nothing touches CRM state, channels, or external systems. What the operator
// debugs is exactly what production would have done.

// MaxSimulationHistory bounds the transcript a client may replay per turn. The
// simulator is stateless server-side; the client resends the conversation.
const MaxSimulationHistory = 60

var (
	ErrSimulationMessageRequired = errors.New("agent simulation: message is required")
	ErrSimulationHistoryTooLong  = errors.New("agent simulation: history is too long")
	ErrSimulationMemoriesTooLong = errors.New("agent simulation: too many session memories")
)

type SimulationRole string

const (
	SimulationRoleUser      SimulationRole = "user"      // the operator, playing the lead
	SimulationRoleAssistant SimulationRole = "assistant" // the agent's earlier replies
)

// SimulationMessage is one prior line of the simulated conversation.
type SimulationMessage struct {
	Role    SimulationRole
	Content string
}

// SessionMemory is a memory the agent "saved" earlier in this simulated
// session. Sandbox interception means nothing persists server-side, so the
// client folds the intercepted manage_lead_memory calls and replays them
// here; the turn injects them like real memories, which makes the READ half
// of the memory loop debuggable too. ID is client-chosen (sanitized) so the
// short ids the prompt renders stay stable across turns.
type SessionMemory struct {
	ID       string
	Content  string
	Category string
}

// SimulatedToolCall is one tool invocation the model made during the turn,
// with the sandbox's canned result. Arguments are the model's own: that is
// the thing being debugged.
type SimulatedToolCall struct {
	Name      string
	Arguments map[string]interface{}
	Result    string
	IsError   bool
}

// SimulationDebug is the turn's X-ray: what the model was actually given and
// what it cost. SystemPrompt is the fully assembled prompt (agent prompt +
// channel identity + RAG + memories), operator-visible by design, since the
// person tuning the agent owns every part of it.
type SimulationDebug struct {
	Model            string
	SystemPrompt     string
	ToolNames        []string
	MemoryInjected   bool
	RAGInjected      bool
	PromptTokens     int
	CompletionTokens int
	FinishReason     string
}

type SimulateTurnInput struct {
	WorkspaceID string
	AgentID     string

	// History is the conversation so far (client-held; server is stateless).
	History []SimulationMessage
	// Message is the operator's new line, spoken as the lead.
	Message string

	// LeadID optionally simulates "talking to this lead": their real memories
	// are injected read-only, exactly as production would. Empty simulates an
	// unknown contact.
	LeadID string

	// SessionMemories are the facts remembered earlier in THIS simulated
	// session (client-held, like the history). Bounded by MaxSimulationHistory.
	SessionMemories []SessionMemory
}

type SimulateTurnOutput struct {
	// Replies are the agent's reply bubbles (segmented exactly as the
	// messaging channels would deliver them).
	Replies   []string
	ToolCalls []SimulatedToolCall
	Debug     SimulationDebug
}

type SimulateTurnUseCase interface {
	Execute(ctx context.Context, in SimulateTurnInput) (*SimulateTurnOutput, error)
}
