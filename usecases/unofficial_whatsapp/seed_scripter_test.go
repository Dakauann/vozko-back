package unofficial_whatsapp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"vozko/domain/ai"
	uw "vozko/domain/unofficial_whatsapp"
)

type fakeAIService struct {
	mu     sync.Mutex
	inputs []ai.GenerateInput
	output *ai.GenerateOutput
	err    error
}

func (f *fakeAIService) Generate(_ context.Context, in ai.GenerateInput) (*ai.GenerateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, in)
	if f.err != nil {
		return nil, f.err
	}
	return f.output, nil
}

func (f *fakeAIService) GenerateStream(context.Context, ai.GenerateInput) (<-chan ai.StreamEvent, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeAIService) GetAvaibleModels(context.Context) ([]string, error) { return nil, nil }
func (f *fakeAIService) GetModelsWithPricing(context.Context) ([]ai.ModelInfo, error) {
	return nil, nil
}

func (f *fakeAIService) lastInput(t *testing.T) ai.GenerateInput {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.inputs) == 0 {
		t.Fatal("the provider was never called")
	}
	return f.inputs[len(f.inputs)-1]
}

func answering(content string) *fakeAIService {
	return &fakeAIService{output: &ai.GenerateOutput{
		Message:      ai.Message{Role: ai.RoleAssistant, Content: content},
		FinishReason: "stop",
		Usage:        ai.Usage{PromptTokens: 410, CompletionTokens: 220},
	}}
}

func sampleScriptRequest() uw.ScriptRequest {
	return uw.ScriptRequest{
		WorkspaceID: "ws-1",
		Model:       "openai/gpt-4o-mini",
		Context:     "curso tecnico de enfermagem",
		MaxMessages: 4,
		Subjects: []uw.ScriptSubject{
			{Ref: 1, Name: "Marina", FirstMessage: "Oi Marina, tudo bem?"},
			{Ref: 2, Name: "Joao", FirstMessage: "Oi Joao, tudo bem?"},
		},
	}
}

const twoThreads = `{"threads":[
  {"ref":1,"turns":[{"fromLead":true,"text":"oi! vi sim"},{"fromLead":false,"text":"Claro."}]},
  {"ref":2,"turns":[{"fromLead":true,"text":"opa"}]}
]}`

func TestScripterSetsEveryProviderGuard(t *testing.T) {
	svc := answering(twoThreads)
	res, err := NewConversationScripter(svc, "fallback/model").Script(context.Background(), sampleScriptRequest())
	if err != nil {
		t.Fatalf("Script: %v", err)
	}
	if res == nil {
		t.Fatal("no result")
	}

	in := svc.lastInput(t)
	if in.WorkspaceID != "ws-1" {
		t.Error("WorkspaceID missing from GenerateInput: this is the revenue leak")
	}
	if in.Model != "openai/gpt-4o-mini" {
		t.Errorf("model = %q, want the request's", in.Model)
	}
	if in.Temperature <= 0 {
		t.Errorf("temperature = %v, want it warm enough to vary", in.Temperature)
	}
	if in.MaxTokens <= 0 {
		t.Error("MaxTokens is unset: a runaway generation would be unbounded")
	}
	if in.ReasoningMaxTokens != scriptReasoningCap {
		t.Errorf("ReasoningMaxTokens = %d, want %d", in.ReasoningMaxTokens, scriptReasoningCap)
	}
	if len(in.Tools) != 0 {
		t.Error("tools must be nil: the model returns text, the use case writes it")
	}
	if in.ResponseFormat == nil ||
		in.ResponseFormat.Type != ai.ResponseFormatJSONSchema ||
		!in.ResponseFormat.JSONSchemaStrict {
		t.Fatalf("response format = %+v, want a strict json schema", in.ResponseFormat)
	}
	if in.ResponseFormat.JSONSchema["type"] != "object" {
		t.Error("the schema is not the domain's object schema")
	}
}

func TestScripterDerivesTheTokenCeilingFromTheAsk(t *testing.T) {
	small := answering(twoThreads)
	req := sampleScriptRequest()
	if _, err := NewConversationScripter(small, "m").Script(context.Background(), req); err != nil {
		t.Fatalf("Script: %v", err)
	}
	smallCeiling := small.lastInput(t).MaxTokens

	big := answering(twoThreads)
	req.MaxMessages = uw.ScriptMaxMessages
	req.Subjects = append(req.Subjects,
		uw.ScriptSubject{Ref: 3, Name: "Ana", FirstMessage: "Oi Ana"},
		uw.ScriptSubject{Ref: 4, Name: "Bia", FirstMessage: "Oi Bia"},
		uw.ScriptSubject{Ref: 5, Name: "Caio", FirstMessage: "Oi Caio"},
	)
	if _, err := NewConversationScripter(big, "m").Script(context.Background(), req); err != nil {
		t.Fatalf("Script: %v", err)
	}
	if big.lastInput(t).MaxTokens <= smallCeiling {
		t.Fatalf("a bigger ask got %d tokens, no more than the smaller ask's %d",
			big.lastInput(t).MaxTokens, smallCeiling)
	}
}

func TestScripterFallsBackToTheDefaultModel(t *testing.T) {
	svc := answering(twoThreads)
	req := sampleScriptRequest()
	req.Model = "   "
	if _, err := NewConversationScripter(svc, "fallback/model").Script(context.Background(), req); err != nil {
		t.Fatalf("Script: %v", err)
	}
	if got := svc.lastInput(t).Model; got != "fallback/model" {
		t.Fatalf("model = %q, want the deployment default", got)
	}
}

func TestScripterRefusesWithoutAWorkspace(t *testing.T) {
	svc := answering(twoThreads)
	req := sampleScriptRequest()
	req.WorkspaceID = "  "
	if _, err := NewConversationScripter(svc, "m").Script(context.Background(), req); err == nil {
		t.Fatal("a call with no workspace was allowed")
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.inputs) != 0 {
		t.Fatalf("the provider was called %d times for a workspaceless request", len(svc.inputs))
	}
}

func TestScripterRefusesWithNoSubjects(t *testing.T) {
	svc := answering(twoThreads)
	req := sampleScriptRequest()
	req.Subjects = nil
	if _, err := NewConversationScripter(svc, "m").Script(context.Background(), req); err == nil {
		t.Fatal("a call with no subjects was allowed")
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.inputs) != 0 {
		t.Fatal("the provider was called with nothing to write")
	}
}

func TestScripterParsesThreadsByRef(t *testing.T) {
	res, err := NewConversationScripter(answering(twoThreads), "m").
		Script(context.Background(), sampleScriptRequest())
	if err != nil {
		t.Fatalf("Script: %v", err)
	}
	if len(res.Threads) != 2 {
		t.Fatalf("parsed %d threads, want 2", len(res.Threads))
	}
	if res.Threads[0].Ref != 1 || len(res.Threads[0].Turns) != 2 {
		t.Errorf("thread 0 = %+v", res.Threads[0])
	}
	if !res.Threads[0].Turns[0].FromLead || res.Threads[0].Turns[0].Text != "oi! vi sim" {
		t.Errorf("first turn = %+v, want the lead's", res.Threads[0].Turns[0])
	}
	if res.PromptTokens != 410 || res.CompletionTokens != 220 {
		t.Errorf("usage = %d/%d, want 410/220", res.PromptTokens, res.CompletionTokens)
	}
	if res.Model != "openai/gpt-4o-mini" {
		t.Errorf("model = %q", res.Model)
	}
}

func TestScripterParsesAFencedResponse(t *testing.T) {
	fenced := "```json\n" + twoThreads + "\n```"
	res, err := NewConversationScripter(answering(fenced), "m").
		Script(context.Background(), sampleScriptRequest())
	if err != nil {
		t.Fatalf("Script: %v", err)
	}
	if len(res.Threads) != 2 {
		t.Fatalf("parsed %d threads from a fenced response, want 2", len(res.Threads))
	}
}

func TestScripterDoesNotParseATruncatedResponse(t *testing.T) {
	svc := answering(`{"threads":[{"ref":1,"turns":[{"fromLead":true,"te`)
	svc.output.FinishReason = "length"

	res, err := NewConversationScripter(svc, "m").Script(context.Background(), sampleScriptRequest())
	if err != nil {
		t.Fatalf("Script: %v", err)
	}
	if len(res.Threads) != 0 {
		t.Fatalf("parsed %d threads from a truncated response", len(res.Threads))
	}
	if res.FinishReason != "length" {
		t.Errorf("finish reason = %q, want it reported", res.FinishReason)
	}
}

func TestScripterReportsUnparseableOutput(t *testing.T) {
	if _, err := NewConversationScripter(answering("desculpe, nao posso ajudar"), "m").
		Script(context.Background(), sampleScriptRequest()); err == nil {
		t.Fatal("unparseable output was accepted")
	}
	if _, err := NewConversationScripter(answering("   "), "m").
		Script(context.Background(), sampleScriptRequest()); err == nil {
		t.Fatal("an empty response was accepted")
	}
}

func TestScripterPropagatesAProviderError(t *testing.T) {
	svc := &fakeAIService{err: errors.New("upstream 503")}
	if _, err := NewConversationScripter(svc, "m").Script(context.Background(), sampleScriptRequest()); err == nil {
		t.Fatal("a provider error was swallowed")
	}
}

func TestScripterWithoutAServiceRefusesCleanly(t *testing.T) {
	if got := NewConversationScripter(nil, "m"); got != nil {
		t.Fatal("a scripter was built over a nil AI service; it must be nil so the caller skips it")
	}
}

func TestScriptPromptStatesTheRulesThatKeepTheseRowsSafe(t *testing.T) {
	prompt := buildScriptSystemPrompt("curso tecnico de enfermagem", 4)
	lowered := strings.ToLower(prompt)

	for _, forbidden := range []string{"pre", "data", "link", "pagamento"} {
		if !strings.Contains(lowered, forbidden) {
			t.Errorf("the prompt never mentions %q; the refusal list is incomplete", forbidden)
		}
	}
	if !strings.Contains(prompt, "curso tecnico de enfermagem") {
		t.Error("the operator's context did not reach the prompt")
	}
	if !strings.Contains(prompt, "\"\"\"") {
		t.Error("the operator's context is not quoted; it can be read as an instruction")
	}
	if !strings.Contains(prompt, "3") {
		t.Error("the prompt never states how many replies it may write")
	}
}

func TestScriptPromptWithoutContextStillWorks(t *testing.T) {
	prompt := buildScriptSystemPrompt("   ", 6)
	if strings.TrimSpace(prompt) == "" {
		t.Fatal("an empty context produced an empty prompt")
	}
	if strings.Contains(prompt, "\"\"\"\n\n\"\"\"") {
		t.Error("an empty context was quoted as an empty block")
	}
}

func TestScriptUserMessageIsJSON(t *testing.T) {
	msg, err := buildScriptUserMessage([]uw.ScriptSubject{
		{Ref: 1, Name: "Marina \"M\"", FirstMessage: "Oi\nMarina"},
	})
	if err != nil {
		t.Fatalf("buildScriptUserMessage: %v", err)
	}
	if !strings.Contains(msg, `\"M\"`) || !strings.Contains(msg, `\n`) {
		t.Fatalf("message = %q, want the name JSON-escaped", msg)
	}
	if !strings.Contains(msg, `"ref":1`) {
		t.Errorf("message = %q, want the ref carried", msg)
	}
}
