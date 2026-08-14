package conversation_usecase

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/conversation"
	leadmemory "vozko/domain/lead_memory"
	"vozko/domain/rag"
	"vozko/domain/shared"
	"vozko/domain/tools"
	"vozko/usecases/agentturn"
)

// Before the assembler was adopted, this service sent the agent's raw prompt and
// nothing else: no tools, no knowledge base, no channel identity, while the
// WhatsApp pipeline had all three. An agent configured with a knowledge base in
// the UI silently ignored it on Instagram and Telegram.
//
// These pin what the model actually receives.

type assemblerTestRAG struct{ results []rag.QueryResult }

func (f assemblerTestRAG) Query(context.Context, rag.QueryInput) (*rag.QueryOutput, error) {
	return &rag.QueryOutput{Results: f.results}, nil
}
func (f assemblerTestRAG) QueryForAgent(context.Context, rag.AgentQueryInput) (*rag.QueryOutput, error) {
	return &rag.QueryOutput{Results: f.results}, nil
}

type assemblerTestRegistry struct{ defs []tools.Definition }

func (s assemblerTestRegistry) Definitions() []tools.Definition { return s.defs }
func (s assemblerTestRegistry) DefinitionsFor(tools.ToolVisibility) []tools.Definition {
	return s.defs
}
func (s assemblerTestRegistry) Execute(context.Context, string, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (s assemblerTestRegistry) ExecuteWithConfig(context.Context, string, map[string]interface{}, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (s assemblerTestRegistry) Handler(string) (tools.Handler, bool) { return nil, false }

func telegramReplyRequest() conversation.AIReplyRequest {
	return conversation.AIReplyRequest{
		WorkspaceID:           "ws-1",
		EntryID:               "conv-1",
		EntryType:             shared.EntryTypeTelegram,
		AgentID:               "agent-1",
		AgentResponsesEnabled: true,
		Text:                  "vocês entregam em Recife?",
	}
}

func agentWithTools() *agent.Agent {
	return &agent.Agent{
		ID:               "agent-1",
		WorkspaceID:      "ws-1",
		Name:             "Bia",
		MessagingPrompt:  "Você é a Bia.",
		MessagingModel:   "gpt-x",
		RAGEnabled:       true,
		KnowledgeBaseIDs: []string{"kb-1"},
		InternalTools:    []agent.ToolBinding{{Name: "finish_conversation"}},
	}
}

func newAssembledService(t *testing.T) *ChannelAIReplyService {
	t.Helper()
	s := &ChannelAIReplyService{}
	s.SetAssembler(agentturn.New(
		assemblerTestRegistry{defs: []tools.Definition{{Name: "finish_conversation"}}},
		assemblerTestRAG{results: []rag.QueryResult{
			{DocumentName: "entregas", Content: "Entregamos em todo o Nordeste.", Score: 0.9},
		}},
		nil,
	))
	return s
}

func TestTelegramTurnCarriesToolsSeededWithTheConversation(t *testing.T) {
	s := newAssembledService(t)
	req := telegramReplyRequest()

	in := s.generateInput(context.Background(), req, agentWithTools(),
		[]ai.Message{{Role: ai.RoleUser, Content: req.Text}}, req.Text)

	if len(in.Tools) != 1 || in.Tools[0].Name != "finish_conversation" {
		t.Fatalf("tools = %+v, want the agent's tool", in.Tools)
	}

	// The seeds are what tell a conversation-scoped tool WHICH conversation it
	// is acting on. Missing them, finish_conversation either fails or resolves
	// nothing and reports success.
	cfg := in.ToolConfigs["finish_conversation"]
	if cfg["__entry_id"] != "conv-1" {
		t.Errorf("__entry_id = %v, want the entry being answered", cfg["__entry_id"])
	}
	if cfg["__entry_type"] != string(shared.EntryTypeTelegram) {
		t.Errorf("__entry_type = %v, want telegram", cfg["__entry_type"])
	}
	if cfg["__workspace_id"] != "ws-1" {
		t.Errorf("__workspace_id = %v", cfg["__workspace_id"])
	}
}

// Tool EXECUTION belongs to the ai.Service. If the mode is not set, a tool the
// model calls is returned to us unexecuted and the customer gets a reply that
// describes an action nobody performed.
func TestTelegramTurnLetsTheAIServiceExecuteTools(t *testing.T) {
	s := newAssembledService(t)
	req := telegramReplyRequest()

	in := s.generateInput(context.Background(), req, agentWithTools(), nil, req.Text)

	if in.ToolExecutionMode != ai.ToolExecutionModeAuto {
		t.Errorf("ToolExecutionMode = %q, want auto", in.ToolExecutionMode)
	}
}

func TestTelegramTurnIsGroundedInTheKnowledgeBase(t *testing.T) {
	s := newAssembledService(t)
	req := telegramReplyRequest()

	in := s.generateInput(context.Background(), req, agentWithTools(), nil, req.Text)

	if !strings.Contains(in.SystemPrompt, "Entregamos em todo o Nordeste") {
		t.Errorf("the knowledge base was not injected: %q", in.SystemPrompt)
	}
	if !strings.Contains(in.SystemPrompt, "Você é a Bia.") {
		t.Errorf("the agent prompt is missing: %q", in.SystemPrompt)
	}
}

// The preamble must say "messaging", not "whatsapp": told it is on WhatsApp, an
// agent in a Telegram chat offers to "send that to your WhatsApp".
func TestTelegramTurnDeclaresTheMessagingChannel(t *testing.T) {
	s := newAssembledService(t)
	req := telegramReplyRequest()

	in := s.generateInput(context.Background(), req, agentWithTools(), nil, req.Text)

	if !strings.Contains(in.SystemPrompt, "Conversation Context") {
		t.Errorf("no identity preamble: %q", in.SystemPrompt)
	}
	if strings.Contains(in.SystemPrompt, "CANAL: WHATSAPP") {
		t.Errorf("a Telegram conversation was told it is on WhatsApp: %q", in.SystemPrompt)
	}
}

// History already ends with the message being answered. Passing it again as
// UserMessage would duplicate the customer's last line.
func TestTelegramTurnDoesNotDuplicateTheLastMessage(t *testing.T) {
	s := newAssembledService(t)
	req := telegramReplyRequest()
	history := []ai.Message{
		{Role: ai.RoleUser, Content: "oi"},
		{Role: ai.RoleAssistant, Content: "olá!"},
		{Role: ai.RoleUser, Content: req.Text},
	}

	in := s.generateInput(context.Background(), req, agentWithTools(), history, req.Text)

	if len(in.Messages) != len(history) {
		t.Fatalf("messages = %d, want %d, the last turn was duplicated", len(in.Messages), len(history))
	}
	if in.Messages[len(in.Messages)-1].Content != req.Text {
		t.Errorf("last message = %q", in.Messages[len(in.Messages)-1].Content)
	}
}

// An unwired container must still answer, exactly as it did before adoption.
func TestWithoutAnAssemblerTheServiceFallsBackToAPlainPrompt(t *testing.T) {
	s := &ChannelAIReplyService{} // no assembler
	req := telegramReplyRequest()

	in := s.generateInput(context.Background(), req, agentWithTools(), nil, req.Text)

	if in.SystemPrompt != "Você é a Bia." {
		t.Errorf("SystemPrompt = %q, want the raw agent prompt", in.SystemPrompt)
	}
	if len(in.Tools) != 0 {
		t.Errorf("tools = %+v, want none without an assembler", in.Tools)
	}
	if in.Model != "gpt-x" || in.WorkspaceID != "ws-1" {
		t.Errorf("model/workspace not carried: %+v", in)
	}
}

// Instagram takes the identical path, the point of the shared recipe is that
// no channel gets a different answer.
func TestInstagramGetsTheSameCapabilitiesAsTelegram(t *testing.T) {
	s := newAssembledService(t)
	req := telegramReplyRequest()
	req.EntryType = shared.EntryTypeInstagram

	in := s.generateInput(context.Background(), req, agentWithTools(), nil, req.Text)

	if len(in.Tools) != 1 {
		t.Errorf("tools = %+v, want the same as Telegram", in.Tools)
	}
	if in.ToolConfigs["finish_conversation"]["__entry_type"] != string(shared.EntryTypeInstagram) {
		t.Error("the seed must carry the channel actually being answered")
	}
	if !strings.Contains(in.SystemPrompt, "Entregamos em todo o Nordeste") {
		t.Error("Instagram must be grounded too")
	}
}

// --- lead memory parity ---
//
// The memory block must reach EVERY channel through the same recipe. WhatsApp
// has its own turn assembly; this pins the channel-agnostic path (Telegram,
// Instagram, unofficial WhatsApp) so a lead-linked conversation carries the
// block and the tool seeds, and an unlinked one degrades to no block.

type assemblerTestMemories struct{ items []leadmemory.MemoryView }

func (s assemblerTestMemories) Execute(context.Context, leadmemory.ListInput) (*leadmemory.ListResult, error) {
	return &leadmemory.ListResult{Items: s.items, Total: int64(len(s.items))}, nil
}

func newAssembledServiceWithMemories(t *testing.T) *ChannelAIReplyService {
	t.Helper()
	s := &ChannelAIReplyService{}
	s.SetAssembler(agentturn.New(
		assemblerTestRegistry{defs: []tools.Definition{{Name: "finish_conversation"}}},
		assemblerTestRAG{},
		assemblerTestMemories{items: []leadmemory.MemoryView{{
			LeadMemory: &leadmemory.LeadMemory{
				ID:       "11111111-2222-4333-8444-555555555555",
				Category: leadmemory.CategoryPreference,
				Content:  "Prefere boleto a PIX.",
			},
		}}},
	))
	return s
}

func TestChannelTurnWithLeadCarriesMemoryBlockAndSeeds(t *testing.T) {
	s := newAssembledServiceWithMemories(t)
	req := telegramReplyRequest()
	leadID := "lead-1"
	req.LeadID = &leadID

	in := s.generateInput(context.Background(), req, agentWithTools(),
		[]ai.Message{{Role: ai.RoleUser, Content: req.Text}}, req.Text)

	if !strings.Contains(in.SystemPrompt, "Memórias sobre este lead") ||
		!strings.Contains(in.SystemPrompt, "Prefere boleto a PIX.") {
		t.Fatalf("memory block missing from channel prompt:\n%s", in.SystemPrompt)
	}

	cfg := in.ToolConfigs["finish_conversation"]
	if cfg["__lead_id"] != "lead-1" {
		t.Errorf("__lead_id = %v, want the bridged lead", cfg["__lead_id"])
	}
	// Attribution seed: a memory the agent writes must say WHICH agent.
	if cfg["__agent_id"] != "agent-1" {
		t.Errorf("__agent_id = %v", cfg["__agent_id"])
	}
}

func TestChannelTurnWithoutLeadHasNoMemoryBlock(t *testing.T) {
	s := newAssembledServiceWithMemories(t)
	req := telegramReplyRequest() // LeadID nil: not bridged yet

	in := s.generateInput(context.Background(), req, agentWithTools(),
		[]ai.Message{{Role: ai.RoleUser, Content: req.Text}}, req.Text)

	if strings.Contains(in.SystemPrompt, "Memórias sobre este lead") {
		t.Fatalf("memory block rendered without a lead:\n%s", in.SystemPrompt)
	}
	if _, ok := in.ToolConfigs["finish_conversation"]["__lead_id"]; ok {
		t.Error("__lead_id seeded without a lead")
	}
}
