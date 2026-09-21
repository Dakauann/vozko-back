package agent

import (
	"context"
	"errors"
)

const MaxSimulationHistory = 60

var (
	ErrSimulationMessageRequired = errors.New("agent simulation: message is required")
	ErrSimulationHistoryTooLong  = errors.New("agent simulation: history is too long")
	ErrSimulationMemoriesTooLong = errors.New("agent simulation: too many session memories")
)

type SimulationRole string

const (
	SimulationRoleUser      SimulationRole = "user"
	SimulationRoleAssistant SimulationRole = "assistant"
)

type SimulationMessage struct {
	Role    SimulationRole
	Content string
}

type SessionMemory struct {
	ID       string
	Content  string
	Category string
}

type SimulatedToolCall struct {
	Name      string
	Arguments map[string]interface{}
	Result    string
	IsError   bool
	Stubbed   bool
}

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

	History []SimulationMessage
	Message string

	LeadID string

	SessionMemories []SessionMemory
}

type SimulateTurnOutput struct {
	Replies   []string
	ToolCalls []SimulatedToolCall
	Debug     SimulationDebug
}

type SimulateTurnUseCase interface {
	Execute(ctx context.Context, in SimulateTurnInput) (*SimulateTurnOutput, error)
}
