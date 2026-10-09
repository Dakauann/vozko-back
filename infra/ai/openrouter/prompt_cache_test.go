package openrouter

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	openrouter "github.com/revrost/go-openrouter"

	"vozko/domain/ai"
	"vozko/domain/tools"
)

var turnStart = time.Date(2026, 10, 8, 19, 21, 47, 0, time.UTC)

func loopInput(model string) ai.GenerateInput {
	return ai.GenerateInput{
		Model:        model,
		SystemPrompt: "Você é a Elo.",
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: "PEDIDO DO USUÁRIO:\nanime as cenas"},
			{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "c1", Name: "studio_read"}}},
			{Role: ai.RoleTool, ToolCallID: "c1", Content: `{"clips":3}`},
			{Role: ai.RoleAssistant, Content: "Vou editar.", ToolCalls: []ai.ToolCall{{ID: "c2", Name: "studio_edit"}}},
			{Role: ai.RoleTool, ToolCallID: "c2", Content: `{"ok":true}`},
			{Role: ai.RoleUser, Content: "OBSERVAÇÃO DO SISTEMA"},
		},
		Tools:             []tools.Definition{{Name: "studio_read"}, {Name: "studio_edit"}},
		ToolExecutionMode: ai.ToolExecutionModeNone,
		SessionID:         "aichat:th-1",
		AsOf:              turnStart,
		VolatileTail:      1,
	}
}

func marked(content openrouter.Content) bool {
	for _, part := range content.Multi {
		if part.CacheControl != nil {
			return true
		}
	}
	return false
}

func TestThePromptPrefixStaysTheSameForEveryCallOfATurn(t *testing.T) {
	s := mustService(t, Config{DefaultModel: "openai/gpt-5"}, nil)
	first := s.buildRequest(loopInput("openai/gpt-5"))
	later := loopInput("openai/gpt-5")
	later.Messages = append(later.Messages, ai.Message{Role: ai.RoleAssistant, Content: "Pronto."})
	second := s.buildRequest(later)

	system := first.Messages[0].Content.Text
	if system != "Você é a Elo." {
		t.Fatalf("with a volatile tail the system message is only the static prompt, got %q", system)
	}
	if tail := first.Messages[len(first.Messages)-1].Content.Text; !strings.HasSuffix(tail, "08/10/2026 16:21]") {
		t.Fatalf("the clock rides in the volatile tail, in Brazil time, to the minute, got %q", tail)
	}
	if second.Messages[0].Content.Text != system {
		t.Fatal("every call of a turn must send the same system message")
	}
	if first.SessionId != "aichat:th-1" {
		t.Fatalf("session = %q", first.SessionId)
	}
	body, _ := json.Marshal(first)
	if strings.Contains(string(body), "cache_control") {
		t.Fatal("providers that cache on their own get no markers")
	}
}

func TestClaudeGetsBreakpointsOnTheStaticSystemAndTheRollingTail(t *testing.T) {
	s := mustService(t, Config{DefaultModel: "anthropic/claude-sonnet-5.5"}, nil)
	req := s.buildRequest(loopInput("anthropic/claude-sonnet-5.5"))

	system := req.Messages[0].Content.Multi
	if len(system) != 1 || system[0].Text != "Você é a Elo." || system[0].CacheControl == nil {
		t.Fatalf("system = %+v", system)
	}
	if tail := req.Messages[len(req.Messages)-1].Content.Text; !strings.HasSuffix(tail, "08/10/2026 16:21]") {
		t.Fatalf("clock in the tail = %q", tail)
	}
	tail := req.Messages[1:]
	want := []bool{false, false, false, true, true, false}
	for i, message := range tail {
		if marked(message.Content) != want[i] {
			t.Fatalf("message %d (%s) marked = %v, want %v", i, message.Role, marked(message.Content), want[i])
		}
	}
	body, _ := json.Marshal(req)
	if count := strings.Count(string(body), "cache_control"); count != 3 {
		t.Fatalf("expected 3 breakpoints, got %d", count)
	}
	if strings.Contains(string(body), `"content":null`) {
		t.Fatal("a marked message must keep its text")
	}
}

func TestClaudeNeverMarksAnEmptyMessage(t *testing.T) {
	s := mustService(t, Config{DefaultModel: "anthropic/claude-sonnet-5.5"}, nil)
	input := loopInput("anthropic/claude-sonnet-5.5")
	input.Messages = []ai.Message{
		{Role: ai.RoleUser, Content: "oi"},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "c1", Name: "studio_read"}}},
		{Role: ai.RoleTool, ToolCallID: "c1", Content: "   "},
		{Role: ai.RoleUser, Content: "OBSERVAÇÃO DO SISTEMA"},
	}
	req := s.buildRequest(input)
	if !marked(req.Messages[1].Content) || marked(req.Messages[2].Content) || marked(req.Messages[3].Content) {
		t.Fatal("only the user message has text to carry a breakpoint")
	}
}

func TestWithoutAVolatileTailTheClockClosesTheSystemPrompt(t *testing.T) {
	s := mustService(t, Config{DefaultModel: "anthropic/claude-sonnet-5.5"}, nil)
	input := loopInput("anthropic/claude-sonnet-5.5")
	input.VolatileTail = 0
	req := s.buildRequest(input)
	system := req.Messages[0].Content.Multi
	if len(system) != 2 || system[0].CacheControl == nil || !strings.HasSuffix(system[1].Text, "08/10/2026 16:21]") {
		t.Fatalf("system = %+v", system)
	}
	plain := mustService(t, Config{DefaultModel: "openai/gpt-5"}, nil).buildRequest(ai.GenerateInput{Model: "openai/gpt-5", SystemPrompt: "Oi.", AsOf: turnStart, Messages: []ai.Message{{Role: ai.RoleUser, Content: "x"}}})
	if got := plain.Messages[0].Content.Text; !strings.HasPrefix(got, "Oi.") || !strings.HasSuffix(got, "08/10/2026 16:21]") {
		t.Fatalf("system = %q", got)
	}
}
