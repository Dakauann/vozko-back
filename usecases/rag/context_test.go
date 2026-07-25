package rag_usecase

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/rag"
)

type fakeRAG struct {
	agentResults []rag.QueryResult
	kbResults    []rag.QueryResult
	agentCalls   int
	kbCalls      int
	gotKBIDs     []string
}

func (f *fakeRAG) Query(ctx context.Context, in rag.QueryInput) (*rag.QueryOutput, error) {
	f.kbCalls++
	f.gotKBIDs = in.KnowledgeBaseIDs
	return &rag.QueryOutput{Results: f.kbResults}, nil
}

func (f *fakeRAG) QueryForAgent(ctx context.Context, in rag.AgentQueryInput) (*rag.QueryOutput, error) {
	f.agentCalls++
	return &rag.QueryOutput{Results: f.agentResults}, nil
}

func chunk(name, content string, score float32) rag.QueryResult {
	return rag.QueryResult{DocumentName: name, Content: content, Score: score}
}

func TestBuildContext_AgentRAGWinsOverExplicitKBs(t *testing.T) {
	f := &fakeRAG{
		agentResults: []rag.QueryResult{chunk("precos.csv", "Administração: R$ 500", 0.9)},
		kbResults:    []rag.QueryResult{chunk("other", "SHOULD NOT BE USED", 0.9)},
	}
	ag := &agent.Agent{ID: "a1", RAGEnabled: true, KnowledgeBaseIDs: []string{"kb1"}}

	out := BuildContext(context.Background(), f, ContextInput{
		Agent:            ag,
		KnowledgeBaseIDs: []string{"kbX"}, // ignored: agent RAG takes precedence
		Query:            "preço de administração?",
	})

	if f.agentCalls != 1 || f.kbCalls != 0 {
		t.Fatalf("expected agent query only, got agent=%d kb=%d", f.agentCalls, f.kbCalls)
	}
	if !strings.Contains(out, "Administração: R$ 500") {
		t.Fatalf("expected agent chunk in output, got: %q", out)
	}
	if !strings.Contains(out, "Contexto adicional") {
		t.Fatalf("expected grounding preamble, got: %q", out)
	}
}

func TestBuildContext_ExplicitKBFallback(t *testing.T) {
	f := &fakeRAG{kbResults: []rag.QueryResult{chunk("precos.csv", "Contábeis: R$ 400", 0.8)}}
	ag := &agent.Agent{ID: "a1"} // RAG not enabled

	out := BuildContext(context.Background(), f, ContextInput{
		Agent:            ag,
		KnowledgeBaseIDs: []string{"kb1", "kb2"},
		Query:            "preço?",
	})

	if f.kbCalls != 1 || f.agentCalls != 0 {
		t.Fatalf("expected kb query only, got agent=%d kb=%d", f.agentCalls, f.kbCalls)
	}
	if len(f.gotKBIDs) != 2 {
		t.Fatalf("expected 2 KB IDs forwarded, got %v", f.gotKBIDs)
	}
	if !strings.Contains(out, "Contábeis: R$ 400") {
		t.Fatalf("expected kb chunk, got: %q", out)
	}
}

func TestBuildContext_NoOpCases(t *testing.T) {
	f := &fakeRAG{}
	if got := BuildContext(context.Background(), f, ContextInput{Agent: &agent.Agent{}, Query: "x"}); got != "" {
		t.Fatalf("expected empty when nothing applies, got %q", got)
	}
	if got := BuildContext(context.Background(), f, ContextInput{KnowledgeBaseIDs: []string{"kb1"}, Query: "  "}); got != "" {
		t.Fatalf("expected empty for blank query, got %q", got)
	}
	if got := BuildContext(context.Background(), nil, ContextInput{KnowledgeBaseIDs: []string{"kb1"}, Query: "x"}); got != "" {
		t.Fatalf("expected empty for nil service, got %q", got)
	}
	if f.kbCalls != 0 || f.agentCalls != 0 {
		t.Fatalf("expected no queries in no-op cases, got agent=%d kb=%d", f.agentCalls, f.kbCalls)
	}
}

func TestFormatRAGContext(t *testing.T) {
	if got := FormatRAGContext(nil); got != "" {
		t.Fatalf("expected empty for no results, got %q", got)
	}
	out := FormatRAGContext([]rag.QueryResult{chunk("doc", "hello world", 0.75)})
	for _, want := range []string{"Contexto adicional", "hello world", "Fonte: doc"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output, got: %q", want, out)
		}
	}
}
