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

func TestWithoutAnAssemblerTheServiceFallsBackToAPlainPrompt(t *testing.T) {
	s := &ChannelAIReplyService{}
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
	if cfg["__agent_id"] != "agent-1" {
		t.Errorf("__agent_id = %v", cfg["__agent_id"])
	}
}

func TestChannelTurnWithoutLeadHasNoMemoryBlock(t *testing.T) {
	s := newAssembledServiceWithMemories(t)
	req := telegramReplyRequest()

	in := s.generateInput(context.Background(), req, agentWithTools(),
		[]ai.Message{{Role: ai.RoleUser, Content: req.Text}}, req.Text)

	if strings.Contains(in.SystemPrompt, "Memórias sobre este lead") {
		t.Fatalf("memory block rendered without a lead:\n%s", in.SystemPrompt)
	}
	if _, ok := in.ToolConfigs["finish_conversation"]["__lead_id"]; ok {
		t.Error("__lead_id seeded without a lead")
	}
}
