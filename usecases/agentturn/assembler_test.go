package agentturn

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/ai"
	leadmemory "vozko/domain/lead_memory"
	"vozko/domain/rag"
	"vozko/domain/tools"
	shared_usecase "vozko/usecases/shared"
)

type fakeRAG struct{ results []rag.QueryResult }

func (f fakeRAG) Query(ctx context.Context, in rag.QueryInput) (*rag.QueryOutput, error) {
	return &rag.QueryOutput{Results: f.results}, nil
}
func (f fakeRAG) QueryForAgent(ctx context.Context, in rag.AgentQueryInput) (*rag.QueryOutput, error) {
	return &rag.QueryOutput{Results: f.results}, nil
}

type stubRegistry struct{ defs []tools.Definition }

func (s stubRegistry) Definitions() []tools.Definition                          { return s.defs }
func (s stubRegistry) DefinitionsFor(v tools.ToolVisibility) []tools.Definition { return s.defs }
func (s stubRegistry) Execute(ctx context.Context, name string, params map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (s stubRegistry) ExecuteWithConfig(ctx context.Context, name string, config, params map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
func (s stubRegistry) Handler(name string) (tools.Handler, bool) { return nil, false }

func TestAssemble_PromptOrderingAndRAG(t *testing.T) {
	a := New(nil, fakeRAG{results: []rag.QueryResult{{DocumentName: "precos", Content: "Admin: R$ 500", Score: 0.9}}}, nil)

	ag := &agent.Agent{
		ID:               "a1",
		WorkspaceID:      "ws1",
		Name:             "Bob",
		MessagingPrompt:  "Você é o Bob.",
		RAGEnabled:       true,
		KnowledgeBaseIDs: []string{"kb1"},
	}
	identity := shared_usecase.ConversationContext{Channel: shared_usecase.ChannelMessaging, AgentName: "Bob"}

	out := a.Assemble(context.Background(), Request{
		Agent:        ag,
		Identity:     &identity,
		RAGQuery:     "preço admin?",
		PromptSuffix: "\n\n[SUFFIX]",
		History:      []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
		UserMessage:  "preço admin?",
		Model:        "gpt-x",
		Temperature:  0.2,
		Segmented:    true,
	})

	sp := out.Input.SystemPrompt
	ctxIdx := strings.Index(sp, "Conversation Context")
	baseIdx := strings.Index(sp, "Você é o Bob.")
	ragIdx := strings.Index(sp, "Admin: R$ 500")
	sufIdx := strings.Index(sp, "[SUFFIX]")
	if ctxIdx < 0 || baseIdx < 0 || ragIdx < 0 || sufIdx < 0 {
		t.Fatalf("a section is missing from system prompt: %q", sp)
	}
	if !(ctxIdx < baseIdx && baseIdx < ragIdx && ragIdx < sufIdx) {
		t.Fatalf("wrong ordering ctx=%d base=%d rag=%d suffix=%d", ctxIdx, baseIdx, ragIdx, sufIdx)
	}

	if out.Input.Model != "gpt-x" || out.Input.WorkspaceID != "ws1" {
		t.Fatalf("model/workspace not propagated: %+v", out.Input)
	}
	if !out.Input.SegmentedResponse || out.Input.Temperature != 0.2 {
		t.Fatalf("generation knobs not propagated: %+v", out.Input)
	}
	if n := len(out.Input.Messages); n != 2 {
		t.Fatalf("expected history + user turn = 2 messages, got %d", n)
	}
	last := out.Input.Messages[1]
	if last.Role != ai.RoleUser || last.Content != "preço admin?" {
		t.Fatalf("last message should be the appended user turn, got %+v", last)
	}
}

func TestAssemble_ResolvesToolsAndStampsSeeds(t *testing.T) {
	reg := stubRegistry{defs: []tools.Definition{{Name: "book_meeting"}}}
	a := New(reg, fakeRAG{}, nil)

	ag := &agent.Agent{
		ID:              "a1",
		WorkspaceID:     "ws1",
		MessagingPrompt: "hi",
		InternalTools:   []agent.ToolBinding{{Name: "book_meeting"}},
	}

	out := a.Assemble(context.Background(), Request{
		Agent:                ag,
		ResolveInternalTools: true,
		Visibility:           agent.ToolVisibilityMessaging,
		ToolSeed:             map[string]interface{}{"__workspace_id": "ws1", "__recipient_phone": "+55"},
	})

	if len(out.Input.Tools) != 1 || out.Input.Tools[0].Name != "book_meeting" {
		t.Fatalf("expected book_meeting tool, got %+v", out.Input.Tools)
	}
	if len(out.ToolNames) != 1 || out.ToolNames[0] != "book_meeting" {
		t.Fatalf("expected resolved tool name, got %v", out.ToolNames)
	}
	cfg := out.Input.ToolConfigs["book_meeting"]
	if cfg["__workspace_id"] != "ws1" || cfg["__recipient_phone"] != "+55" {
		t.Fatalf("channel seeds not stamped onto tool config: %+v", cfg)
	}
}

func TestAssemble_NoRAGWhenDisabled(t *testing.T) {
	a := New(nil, fakeRAG{results: []rag.QueryResult{{Content: "SHOULD NOT APPEAR"}}}, nil)
	ag := &agent.Agent{MessagingPrompt: "hi"} // RAG disabled
	out := a.Assemble(context.Background(), Request{Agent: ag, RAGQuery: "x"})
	if strings.Contains(out.Input.SystemPrompt, "SHOULD NOT APPEAR") {
		t.Fatalf("RAG injected despite disabled agent: %q", out.Input.SystemPrompt)
	}
}

// --- lead memory block ---

type stubMemoryList struct {
	items []leadmemory.MemoryView
}

func (s stubMemoryList) Execute(ctx context.Context, in leadmemory.ListInput) (*leadmemory.ListResult, error) {
	return &leadmemory.ListResult{Items: s.items, Total: int64(len(s.items))}, nil
}

func memoryItem(content string) leadmemory.MemoryView {
	return leadmemory.MemoryView{LeadMemory: &leadmemory.LeadMemory{
		ID:       "11111111-2222-4333-8444-555555555555",
		Category: leadmemory.CategoryPreference,
		Content:  content,
	}}
}

func TestAssemble_MemoryBlockAfterRAGBeforeSuffix(t *testing.T) {
	a := New(nil,
		fakeRAG{results: []rag.QueryResult{{DocumentName: "d", Content: "RAG-CHUNK", Score: 0.9}}},
		stubMemoryList{items: []leadmemory.MemoryView{memoryItem("Prefere boleto.")}})

	ag := &agent.Agent{ID: "a1", WorkspaceID: "ws1", MessagingPrompt: "base", RAGEnabled: true, KnowledgeBaseIDs: []string{"kb"}}
	out := a.Assemble(context.Background(), Request{
		Agent:        ag,
		LeadID:       "lead-1",
		RAGQuery:     "q",
		PromptSuffix: "\n\n[SUFFIX]",
	})

	sp := out.Input.SystemPrompt
	ragIdx := strings.Index(sp, "RAG-CHUNK")
	memIdx := strings.Index(sp, "Memórias sobre este lead")
	factIdx := strings.Index(sp, "Prefere boleto.")
	sufIdx := strings.Index(sp, "[SUFFIX]")
	if ragIdx < 0 || memIdx < 0 || factIdx < 0 || sufIdx < 0 {
		t.Fatalf("a section is missing: %q", sp)
	}
	// The order IS the contract: grounding, then memories, then the caller's tail.
	if !(ragIdx < memIdx && memIdx < sufIdx) {
		t.Fatalf("wrong ordering rag=%d mem=%d suffix=%d", ragIdx, memIdx, sufIdx)
	}
}

func TestAssemble_NoMemoryBlockWithoutLead(t *testing.T) {
	a := New(nil, fakeRAG{}, stubMemoryList{items: []leadmemory.MemoryView{memoryItem("SHOULD NOT APPEAR")}})
	// An Instagram/Telegram conversation not yet bridged to a lead: no LeadID,
	// no block, and no error either.
	out := a.Assemble(context.Background(), Request{Agent: &agent.Agent{MessagingPrompt: "hi"}})
	if strings.Contains(out.Input.SystemPrompt, "SHOULD NOT APPEAR") {
		t.Fatalf("memory injected without a lead: %q", out.Input.SystemPrompt)
	}
}

func TestAssemble_MemoryToolLineFollowsBinding(t *testing.T) {
	mem := stubMemoryList{items: []leadmemory.MemoryView{memoryItem("Prefere boleto.")}}

	// Agent WITHOUT the tool: block renders, tool instruction does not.
	a := New(nil, fakeRAG{}, mem)
	out := a.Assemble(context.Background(), Request{Agent: &agent.Agent{WorkspaceID: "ws1", MessagingPrompt: "hi"}, LeadID: "lead-1"})
	if !strings.Contains(out.Input.SystemPrompt, "Memórias sobre este lead") {
		t.Fatal("memory block missing for agent without the tool")
	}
	if strings.Contains(out.Input.SystemPrompt, "manage_lead_memory") {
		t.Fatal("tool instruction rendered for agent without the tool")
	}

	// Agent WITH the tool bound: instruction appears.
	reg := stubRegistry{defs: []tools.Definition{{Name: "manage_lead_memory"}}}
	a = New(reg, fakeRAG{}, mem)
	out = a.Assemble(context.Background(), Request{
		Agent:                &agent.Agent{WorkspaceID: "ws1", MessagingPrompt: "hi", InternalTools: []agent.ToolBinding{{Name: "manage_lead_memory"}}},
		LeadID:               "lead-1",
		ResolveInternalTools: true,
		Visibility:           agent.ToolVisibilityMessaging,
	})
	if !strings.Contains(out.Input.SystemPrompt, "manage_lead_memory") {
		t.Fatal("tool instruction missing for agent with the tool bound")
	}
}
