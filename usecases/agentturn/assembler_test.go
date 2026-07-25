package agentturn

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/ai"
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
	a := New(nil, fakeRAG{results: []rag.QueryResult{{DocumentName: "precos", Content: "Admin: R$ 500", Score: 0.9}}})

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
	a := New(reg, fakeRAG{})

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
	a := New(nil, fakeRAG{results: []rag.QueryResult{{Content: "SHOULD NOT APPEAR"}}})
	ag := &agent.Agent{MessagingPrompt: "hi"} // RAG disabled
	out := a.Assemble(context.Background(), Request{Agent: ag, RAGQuery: "x"})
	if strings.Contains(out.Input.SystemPrompt, "SHOULD NOT APPEAR") {
		t.Fatalf("RAG injected despite disabled agent: %q", out.Input.SystemPrompt)
	}
}
