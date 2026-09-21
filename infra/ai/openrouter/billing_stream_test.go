package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	openrouter "github.com/revrost/go-openrouter"

	"vozko/domain/ai"
)

type capturingPub struct {
	mu     sync.Mutex
	topics []string
	bodies [][]byte
}

func (p *capturingPub) Publish(topic string, message []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.topics = append(p.topics, topic)
	cp := make([]byte, len(message))
	copy(cp, message)
	p.bodies = append(p.bodies, cp)
	return nil
}
func (p *capturingPub) PublishWithDelay(string, []byte, time.Duration) error { return nil }
func (p *capturingPub) ValidateConnection() error                            { return nil }

func (p *capturingPub) events() []ai.AICompletedEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]ai.AICompletedEvent, 0, len(p.bodies))
	for _, b := range p.bodies {
		var e ai.AICompletedEvent
		_ = json.Unmarshal(b, &e)
		out = append(out, e)
	}
	return out
}

func sseServer(chunks []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

func newTestService(baseURL string, pub *capturingPub) *Service {
	cfg := openrouter.DefaultConfig("test-key")
	cfg.BaseURL = baseURL
	return &Service{
		client:     openrouter.NewClientWithConfig(*cfg),
		billingPub: pub,
	}
}

func drain(t *testing.T, ch <-chan ai.StreamEvent) (reasoning, content string) {
	t.Helper()
	for ev := range ch {
		switch ev.Type {
		case ai.StreamEventReasoning:
			reasoning += ev.Token
		case ai.StreamEventToken:
			content += ev.Token
		case ai.StreamEventError:
			t.Fatalf("unexpected stream error: %v", ev.Error)
		}
	}
	return reasoning, content
}

func TestGenerateStream_BillsReasoningTokensWithinCompletion(t *testing.T) {
	srv := sseServer([]string{
		`{"choices":[{"index":0,"delta":{"reasoning":"vou pensar bastante sobre o melhor fluxo a montar"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"Pronto, criei o fluxo."}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":1200,"completion_tokens":1000,"completion_token_details":{"reasoning_tokens":700},"total_tokens":2200,"cost":0.0123}}`,
	})
	defer srv.Close()

	pub := &capturingPub{}
	svc := newTestService(srv.URL, pub)

	ch, err := svc.GenerateStream(context.Background(), ai.GenerateInput{
		Model:       "anthropic/claude-thinker",
		WorkspaceID: "ws-1",
		Messages:    []ai.Message{{Role: ai.RoleUser, Content: "crie um fluxo"}},
	})
	if err != nil {
		t.Fatalf("GenerateStream: %v", err)
	}

	reasoning, content := drain(t, ch)
	if reasoning == "" {
		t.Fatal("the model's thinking must be streamed as reasoning")
	}
	if content != "Pronto, criei o fluxo." {
		t.Fatalf("content = %q", content)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 billing event, got %d", len(events))
	}
	e := events[0]
	if e.WorkspaceID != "ws-1" {
		t.Errorf("workspace = %q, want ws-1", e.WorkspaceID)
	}
	if e.Model != "anthropic/claude-thinker" {
		t.Errorf("model = %q", e.Model)
	}
	if e.PromptTokens != 1200 {
		t.Errorf("billed prompt_tokens = %d, want 1200", e.PromptTokens)
	}
	if e.CompletionTokens != 1000 {
		t.Errorf("billed completion_tokens = %d, want 1000 (incl. 700 reasoning), thinking must be charged", e.CompletionTokens)
	}
}

func TestGenerateStream_NoUsage_PublishesNothing(t *testing.T) {
	srv := sseServer([]string{
		`{"choices":[{"index":0,"delta":{"content":"oi"}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	})
	defer srv.Close()

	pub := &capturingPub{}
	svc := newTestService(srv.URL, pub)

	ch, err := svc.GenerateStream(context.Background(), ai.GenerateInput{
		Model:       "anthropic/claude-thinker",
		WorkspaceID: "ws-1",
		Messages:    []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})
	if err != nil {
		t.Fatalf("GenerateStream: %v", err)
	}
	_, content := drain(t, ch)
	if content != "oi" {
		t.Fatalf("content = %q", content)
	}
	if got := len(pub.events()); got != 0 {
		t.Fatalf("expected 0 billing events when provider omits usage, got %d", got)
	}
}

func TestGenerateStream_NoWorkspace_PublishesNothing(t *testing.T) {
	srv := sseServer([]string{
		`{"choices":[{"index":0,"delta":{"content":"oi"}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
	})
	defer srv.Close()

	pub := &capturingPub{}
	svc := newTestService(srv.URL, pub)

	ch, err := svc.GenerateStream(context.Background(), ai.GenerateInput{
		Model:    "anthropic/claude-thinker",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})
	if err != nil {
		t.Fatalf("GenerateStream: %v", err)
	}
	drain(t, ch)
	if got := len(pub.events()); got != 0 {
		t.Fatalf("expected 0 billing events when workspace is empty, got %d", got)
	}
}
