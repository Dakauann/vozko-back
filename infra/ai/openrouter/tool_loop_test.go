package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	openrouter "github.com/revrost/go-openrouter"

	"vozko/domain/ai"
	"vozko/domain/tools"
)

const toolCallReply = `{"id":"gen","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"%s","type":"function","function":{"name":"manage_entry_stage","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}`
const textReply = `{"id":"gen","choices":[{"index":0,"message":{"role":"assistant","content":"Pronto."},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}`

type recordedRequest struct {
	Tools      []json.RawMessage `json:"tools"`
	ToolChoice any               `json:"tool_choice"`
	Messages   []json.RawMessage `json:"messages"`
}

func scriptedServer(t *testing.T, replies []string) (*httptest.Server, func() []recordedRequest, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		index := len(bodies)
		bodies = append(bodies, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		reply := replies[min(index, len(replies)-1)]
		reply = strings.Replace(reply, "%s", fmt.Sprintf("c%d", index+1), 1)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	parsed := func() []recordedRequest {
		mu.Lock()
		defer mu.Unlock()
		out := make([]recordedRequest, len(bodies))
		for i, b := range bodies {
			_ = json.Unmarshal([]byte(b), &out[i])
		}
		return out
	}
	raw := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bodies...)
	}
	return srv, parsed, raw
}

func loopService(baseURL string) *Service {
	cfg := openrouter.DefaultConfig("test-key")
	cfg.BaseURL = baseURL
	return &Service{client: openrouter.NewClientWithConfig(*cfg), billingPub: &capturingPub{}, toolService: injectFakeToolService{}, maxToolIterations: 3}
}

func TestTheForcedFinalAnswerKeepsTheToolsSoTheCacheHolds(t *testing.T) {
	srv, requests, raw := scriptedServer(t, []string{toolCallReply, toolCallReply, textReply})
	out, err := loopService(srv.URL).Generate(context.Background(), ai.GenerateInput{
		Model:             "anthropic/claude-sonnet-5.5",
		SystemPrompt:      "Você é um agente.",
		WorkspaceID:       "ws",
		Messages:          []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
		Tools:             []tools.Definition{{Name: "manage_entry_stage", Description: "etapa"}},
		ToolExecutionMode: ai.ToolExecutionModeAuto,
		MaxToolIterations: 1,
	})
	if err != nil || out.Message.Content != "Pronto." {
		t.Fatalf("out = %+v err = %v", out, err)
	}
	sent := requests()
	if len(sent) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(sent))
	}
	final := sent[2]
	if len(final.Tools) != 1 || final.ToolChoice != "none" {
		t.Fatalf("the final call must keep the same tools and forbid calling them, got tools %d choice %v", len(final.Tools), final.ToolChoice)
	}
	bodies := raw()
	if strings.Count(bodies[0], "cache_control") != 2 || strings.Count(bodies[2], "cache_control") != 3 {
		t.Fatalf("breakpoints must follow the newest messages: first %d final %d", strings.Count(bodies[0], "cache_control"), strings.Count(bodies[2], "cache_control"))
	}
	if strings.Contains(string(final.Messages[1]), "cache_control") || !strings.Contains(string(final.Messages[len(final.Messages)-1]), "cache_control") {
		t.Fatal("the old marker on the user message must move to the latest tool result")
	}
}

func TestNoToolsMeansNoToolsEvenWithAutomaticExecution(t *testing.T) {
	srv, requests, _ := scriptedServer(t, []string{textReply})
	if _, err := loopService(srv.URL).Generate(context.Background(), ai.GenerateInput{
		Model:       "openai/gpt-4o",
		WorkspaceID: "ws",
		Messages:    []ai.Message{{Role: ai.RoleUser, Content: "classifique"}},
	}); err != nil {
		t.Fatal(err)
	}
	if sent := requests(); len(sent[0].Tools) != 0 {
		t.Fatalf("a call without tools must not receive the whole registry, got %d tools", len(sent[0].Tools))
	}
}

func TestStreamedToolCallsKeepTheModelsOrder(t *testing.T) {
	first, second, third := &openrouter.ToolCall{ID: "a"}, &openrouter.ToolCall{ID: "b"}, &openrouter.ToolCall{ID: "c"}
	ordered := orderedToolCalls(map[int]*openrouter.ToolCall{2: third, 0: first, 1: second})
	if len(ordered) != 3 || ordered[0].ID != "a" || ordered[1].ID != "b" || ordered[2].ID != "c" {
		t.Fatalf("tool calls must follow their index, got %+v", ordered)
	}
}

func TestAProviderThatIgnoresTheForcedAnswerCannotLoopForever(t *testing.T) {
	srv, requests, _ := scriptedServer(t, []string{toolCallReply})
	out, err := loopService(srv.URL).Generate(context.Background(), ai.GenerateInput{
		Model:             "openai/gpt-4o",
		WorkspaceID:       "ws",
		Messages:          []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
		Tools:             []tools.Definition{{Name: "manage_entry_stage", Description: "etapa"}},
		ToolExecutionMode: ai.ToolExecutionModeAuto,
		MaxToolIterations: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls := len(requests()); calls != 3 {
		t.Fatalf("after the forced answer the loop must stop, got %d calls", calls)
	}
	if len(out.ToolCalls) != 3 {
		t.Fatalf("the ignored call is returned without running, got %d tool calls", len(out.ToolCalls))
	} else if out.ToolCalls[2].Result != nil {
		t.Fatal("a tool requested after the forced answer must not run")
	}
}
