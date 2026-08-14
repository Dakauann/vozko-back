package agent_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/ai"
	leadmemory "vozko/domain/lead_memory"
	"vozko/domain/shared"
	"vozko/domain/tools"
	"vozko/usecases/agentturn"
)

// --- fakes ---

type simAgentRepo struct{ agent *agent.Agent }

func (r simAgentRepo) Create(*agent.Agent) error                  { return nil }
func (r simAgentRepo) Update(string, *agent.Agent) error          { return nil }
func (r simAgentRepo) Delete(string) error                        { return nil }
func (r simAgentRepo) FindByIDs([]string) ([]*agent.Agent, error) { return nil, nil }
func (r simAgentRepo) List(agent.ListAgentsInput) (*shared.PaginatedResult[*agent.AgentListItem], error) {
	return nil, nil
}
func (r simAgentRepo) FindByID(id string) (*agent.Agent, error) {
	if r.agent == nil || r.agent.ID != id {
		return nil, errors.New("not found")
	}
	return r.agent, nil
}

type simRegistry struct{ defs []tools.Definition }

func (s simRegistry) Definitions() []tools.Definition                        { return s.defs }
func (s simRegistry) DefinitionsFor(tools.ToolVisibility) []tools.Definition { return s.defs }
func (s simRegistry) Handler(string) (tools.Handler, bool)                   { return nil, false }
func (s simRegistry) Execute(context.Context, string, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (s simRegistry) ExecuteWithConfig(context.Context, string, map[string]interface{}, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}

type simMemories struct{ items []leadmemory.MemoryView }

func (s simMemories) Execute(context.Context, leadmemory.ListInput) (*leadmemory.ListResult, error) {
	return &leadmemory.ListResult{Items: s.items, Total: int64(len(s.items))}, nil
}

type simAI struct {
	lastInput ai.GenerateInput
	output    *ai.GenerateOutput
	err       error
}

func (s *simAI) Generate(_ context.Context, in ai.GenerateInput) (*ai.GenerateOutput, error) {
	s.lastInput = in
	if s.err != nil {
		return nil, s.err
	}
	return s.output, nil
}
func (s *simAI) GenerateStream(context.Context, ai.GenerateInput) (<-chan ai.StreamEvent, error) {
	return nil, errors.New("not used")
}
func (s *simAI) GetAvaibleModels(context.Context) ([]string, error)           { return nil, nil }
func (s *simAI) GetModelsWithPricing(context.Context) ([]ai.ModelInfo, error) { return nil, nil }

func simAgent() *agent.Agent {
	return &agent.Agent{
		ID:              "agent-1",
		WorkspaceID:     "ws-1",
		Name:            "Bia",
		MessagingPrompt: "Você é a Bia.",
		MessagingModel:  "test/model",
		InternalTools:   []agent.ToolBinding{{Name: "manage_lead_memory"}},
	}
}

func simOutput() *ai.GenerateOutput {
	return &ai.GenerateOutput{
		Messages: []string{"Oi!", "Como posso ajudar?"},
		ToolCalls: []ai.ToolCall{{
			Name:      "manage_lead_memory",
			Arguments: map[string]interface{}{"action": "remember", "content": "Prefere boleto."},
			Result:    &tools.ExecutionResult{Result: "(SIMULAÇÃO) ...", IsError: false},
		}},
		Usage:        ai.Usage{PromptTokens: 100, CompletionTokens: 20},
		FinishReason: "stop",
	}
}

func newSimUC(t *testing.T, repo agent.Repository, aiSvc ai.Service, memories leadmemory.ListUseCase) agent.SimulateTurnUseCase {
	t.Helper()
	assembler := agentturn.New(simRegistry{defs: []tools.Definition{{Name: "manage_lead_memory"}}}, nil, memories)
	uc, err := NewSimulateTurnUseCase(repo, assembler, aiSvc)
	if err != nil {
		t.Fatal(err)
	}
	return uc
}

// --- tests ---

func TestSimulateGuards(t *testing.T) {
	aiSvc := &simAI{output: simOutput()}
	uc := newSimUC(t, simAgentRepo{agent: simAgent()}, aiSvc, nil)

	if _, err := uc.Execute(context.Background(), agent.SimulateTurnInput{
		WorkspaceID: "ws-1", AgentID: "agent-1", Message: "   ",
	}); !errors.Is(err, agent.ErrSimulationMessageRequired) {
		t.Fatalf("blank message = %v", err)
	}

	// A foreign workspace's agent behaves like a missing one.
	if _, err := uc.Execute(context.Background(), agent.SimulateTurnInput{
		WorkspaceID: "ws-OTHER", AgentID: "agent-1", Message: "oi",
	}); !errors.Is(err, agent.ErrAgentNotFound) {
		t.Fatalf("foreign agent = %v", err)
	}

	long := make([]agent.SimulationMessage, agent.MaxSimulationHistory+1)
	if _, err := uc.Execute(context.Background(), agent.SimulateTurnInput{
		WorkspaceID: "ws-1", AgentID: "agent-1", Message: "oi", History: long,
	}); !errors.Is(err, agent.ErrSimulationHistoryTooLong) {
		t.Fatalf("long history = %v", err)
	}
}

func TestSimulateAssemblesTheProductionTurn(t *testing.T) {
	aiSvc := &simAI{output: simOutput()}
	uc := newSimUC(t, simAgentRepo{agent: simAgent()}, aiSvc,
		simMemories{items: []leadmemory.MemoryView{{LeadMemory: &leadmemory.LeadMemory{
			ID: "11111111-2222-4333-8444-555555555555", Category: leadmemory.CategoryPreference, Content: "Prefere boleto."}}}})

	out, err := uc.Execute(context.Background(), agent.SimulateTurnInput{
		WorkspaceID: "ws-1",
		AgentID:     "agent-1",
		Message:     "quanto custa?",
		LeadID:      "lead-1",
		History: []agent.SimulationMessage{
			{Role: agent.SimulationRoleUser, Content: "oi"},
			{Role: agent.SimulationRoleAssistant, Content: "olá!"},
			{Role: agent.SimulationRoleUser, Content: "  "}, // dropped
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	in := aiSvc.lastInput
	// The prompt is the production recipe: agent prompt + identity + memories.
	if !strings.Contains(in.SystemPrompt, "Você é a Bia.") {
		t.Fatalf("agent prompt missing: %q", in.SystemPrompt)
	}
	if !strings.Contains(in.SystemPrompt, "Prefere boleto.") {
		t.Fatalf("lead memories missing: %q", in.SystemPrompt)
	}
	// History + the new line as the final user turn.
	if len(in.Messages) != 3 || in.Messages[2].Content != "quanto custa?" || in.Messages[2].Role != ai.RoleUser {
		t.Fatalf("messages = %+v", in.Messages)
	}
	// Sandboxed execution still runs the real loop.
	if in.ToolExecutionMode != ai.ToolExecutionModeAuto {
		t.Fatalf("execution mode = %q", in.ToolExecutionMode)
	}
	if in.WorkspaceID != "ws-1" || !in.SegmentedResponse || in.Model != "test/model" {
		t.Fatalf("input knobs = %+v", in)
	}
	// Seeds: attribution + the honest simulation flag + the chosen lead.
	cfg := in.ToolConfigs["manage_lead_memory"]
	if cfg["__agent_id"] != "agent-1" || cfg["__simulation"] != true || cfg["__lead_id"] != "lead-1" {
		t.Fatalf("seeds = %+v", cfg)
	}
	if _, hasEntry := cfg["__entry_id"]; hasEntry {
		t.Fatal("a simulated turn must not fabricate an entry id")
	}

	// Output mapping: segmented bubbles, tool calls with canned results, debug.
	if len(out.Replies) != 2 || out.Replies[0] != "Oi!" {
		t.Fatalf("replies = %+v", out.Replies)
	}
	if len(out.ToolCalls) != 1 || out.ToolCalls[0].Name != "manage_lead_memory" ||
		out.ToolCalls[0].Arguments["action"] != "remember" || out.ToolCalls[0].IsError {
		t.Fatalf("tool calls = %+v", out.ToolCalls)
	}
	if !out.Debug.MemoryInjected || out.Debug.RAGInjected {
		t.Fatalf("debug flags = %+v", out.Debug)
	}
	if out.Debug.PromptTokens != 100 || out.Debug.FinishReason != "stop" || out.Debug.Model != "test/model" {
		t.Fatalf("debug = %+v", out.Debug)
	}
	if !strings.Contains(out.Debug.SystemPrompt, "Você é a Bia.") {
		t.Fatal("debug must expose the assembled prompt")
	}
}

func TestSimulateWithoutLeadHasNoMemoryAndNoLeadSeed(t *testing.T) {
	aiSvc := &simAI{output: simOutput()}
	uc := newSimUC(t, simAgentRepo{agent: simAgent()}, aiSvc,
		simMemories{items: []leadmemory.MemoryView{{LeadMemory: &leadmemory.LeadMemory{
			ID: "11111111-2222-4333-8444-555555555555", Category: leadmemory.CategoryOther, Content: "FATO-QUE-NAO-PODE-VAZAR"}}}})

	out, err := uc.Execute(context.Background(), agent.SimulateTurnInput{
		WorkspaceID: "ws-1", AgentID: "agent-1", Message: "oi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Debug.MemoryInjected || strings.Contains(aiSvc.lastInput.SystemPrompt, "FATO-QUE-NAO-PODE-VAZAR") {
		t.Fatal("memories injected without a lead")
	}
	if _, has := aiSvc.lastInput.ToolConfigs["manage_lead_memory"]["__lead_id"]; has {
		t.Fatal("__lead_id seeded without a lead")
	}
}

func TestSimulateFallsBackToSingleReply(t *testing.T) {
	aiSvc := &simAI{output: &ai.GenerateOutput{Message: ai.Message{Role: ai.RoleAssistant, Content: "só uma"}}}
	uc := newSimUC(t, simAgentRepo{agent: simAgent()}, aiSvc, nil)

	out, err := uc.Execute(context.Background(), agent.SimulateTurnInput{
		WorkspaceID: "ws-1", AgentID: "agent-1", Message: "oi",
	})
	if err != nil || len(out.Replies) != 1 || out.Replies[0] != "só uma" {
		t.Fatalf("fallback replies = (%+v, %v)", out, err)
	}
}

func TestSimulateInjectsSessionMemories(t *testing.T) {
	aiSvc := &simAI{output: simOutput()}
	uc := newSimUC(t, simAgentRepo{agent: simAgent()}, aiSvc, nil)

	out, err := uc.Execute(context.Background(), agent.SimulateTurnInput{
		WorkspaceID: "ws-1",
		AgentID:     "agent-1",
		Message:     "voltei!",
		SessionMemories: []agent.SessionMemory{
			{ID: "sim-mem-01", Content: "Gosta de gatos.", Category: "personal"},
			{ID: "!!bad id!!", Content: "Prefere boleto.", Category: "vibes"}, // sanitized: synthetic id + Other
			{Content: "   "}, // dropped
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	sp := aiSvc.lastInput.SystemPrompt
	// The block is rendered by the same renderer real memories use, so the
	// READ half of the loop is debuggable even though nothing persisted.
	if !strings.Contains(sp, "Gosta de gatos.") || !strings.Contains(sp, "Prefere boleto.") {
		t.Fatalf("session memories missing from prompt:\n%s", sp)
	}
	// Stable client id renders as the short prefix the tool can target.
	if !strings.Contains(sp, "[sim-mem-") {
		t.Fatalf("stable session id not rendered:\n%s", sp)
	}
	if !out.Debug.MemoryInjected {
		t.Fatal("MemoryInjected flag must reflect the session block")
	}
	// The agent has manage_lead_memory bound, so the how-to-write line renders.
	if !strings.Contains(sp, "manage_lead_memory") {
		t.Fatalf("tool guidance missing for bound agent:\n%s", sp)
	}
}

func TestSimulateSessionMemoriesCap(t *testing.T) {
	aiSvc := &simAI{output: simOutput()}
	uc := newSimUC(t, simAgentRepo{agent: simAgent()}, aiSvc, nil)

	long := make([]agent.SessionMemory, agent.MaxSimulationHistory+1)
	if _, err := uc.Execute(context.Background(), agent.SimulateTurnInput{
		WorkspaceID: "ws-1", AgentID: "agent-1", Message: "oi", SessionMemories: long,
	}); !errors.Is(err, agent.ErrSimulationMemoriesTooLong) {
		t.Fatalf("over cap = %v", err)
	}
}
