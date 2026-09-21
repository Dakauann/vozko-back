package audience_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/ai"
	ca "vozko/domain/audience"
	"vozko/domain/shared"
)

func samplePlan(n int) ca.BatchPlan {
	items := make([]ca.Item, n)
	for i := range items {
		items[i] = ca.Item{ID: "row-" + itoa(i+1), Text: "comentário " + itoa(i+1)}
	}
	plans, _ := ca.PlanBatches(items, 100, ca.DefaultBudget(), ca.SubjectKindComment)
	return plans[0]
}

func sampleRequest(n int) ca.ClassifyRequest {
	return ca.ClassifyRequest{
		WorkspaceID: "ws-1", Model: "openai/gpt-4o-mini",
		Topics:  ca.DefaultTopicsFor(ca.VerticalGov),
		Context: ca.ContainerContext{Caption: "Asfalto novo"},
		Batch:   samplePlan(n),
	}
}

// T-32. The adapter logs CRITICAL REVENUE LEAK for a call without a
// workspace; this is the test that makes that impossible from here.
func TestClassifier_SetsEveryProviderGuard(t *testing.T) {
	svc := &fakeAI{Output: &ai.GenerateOutput{
		Message: ai.Message{Role: ai.RoleAssistant, Content: `{"results":[]}`}, FinishReason: "stop",
		Usage: ai.Usage{PromptTokens: 321, CompletionTokens: 45},
	}}
	c := NewClassifier(svc, "fallback/model")
	res, err := c.Classify(context.Background(), sampleRequest(20))
	if err != nil {
		t.Fatal(err)
	}
	if len(svc.Inputs) != 1 {
		t.Fatalf("calls = %d", len(svc.Inputs))
	}
	in := svc.Inputs[0]
	if in.WorkspaceID != "ws-1" {
		t.Fatal("WorkspaceID missing from GenerateInput: this is the revenue leak")
	}
	if in.Model != "openai/gpt-4o-mini" {
		t.Errorf("model = %q", in.Model)
	}
	if in.Temperature != 0 {
		t.Errorf("temperature = %v, want 0 (classification, not generation)", in.Temperature)
	}
	if in.MaxTokens != ca.DefaultBudget().MaxTokensForBatch(20) {
		t.Errorf("MaxTokens = %d, want the budget's hard cap %d", in.MaxTokens, ca.DefaultBudget().MaxTokensForBatch(20))
	}
	if in.ReasoningMaxTokens != reasoningCap {
		t.Errorf("ReasoningMaxTokens = %d, want %d", in.ReasoningMaxTokens, reasoningCap)
	}
	if in.ResponseFormat == nil || in.ResponseFormat.Type != ai.ResponseFormatJSONSchema || !in.ResponseFormat.JSONSchemaStrict {
		t.Errorf("response format = %+v, want strict json_schema", in.ResponseFormat)
	}
	if in.ResponseFormat.JSONSchema["type"] != "object" {
		t.Error("schema must be the rubric's object schema")
	}
	if len(in.Tools) != 0 {
		t.Error("tools must be nil: the model returns data, the use case persists")
	}
	if len(in.Messages) != 1 || in.Messages[0].Role != ai.RoleUser || !strings.Contains(in.Messages[0].Content, `"ref":1`) {
		t.Errorf("user message = %+v", in.Messages)
	}
	if !strings.Contains(in.SystemPrompt, "Asfalto novo") || !strings.Contains(in.SystemPrompt, `"saude"`) {
		t.Error("system prompt must carry the caption and the topic keys")
	}
	if res.PromptTokens != 321 || res.CompletionTokens != 45 || res.FinishReason != "stop" {
		t.Errorf("usage not passed through: %+v", res)
	}
}

func TestClassifier_RefusesWithoutWorkspace(t *testing.T) {
	svc := &fakeAI{}
	req := sampleRequest(1)
	req.WorkspaceID = ""
	if _, err := NewClassifier(svc, "m").Classify(context.Background(), req); !errors.Is(err, ca.ErrWorkspaceRequired) {
		t.Fatalf("expected ErrWorkspaceRequired, got %v", err)
	}
	if len(svc.Inputs) != 0 {
		t.Fatal("no call may be made without a workspace")
	}
}

func TestClassifier_FallsBackToDefaultModel(t *testing.T) {
	svc := &fakeAI{}
	req := sampleRequest(1)
	req.Model = ""
	if _, err := NewClassifier(svc, "fallback/model").Classify(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if svc.Inputs[0].Model != "fallback/model" {
		t.Fatalf("model = %q", svc.Inputs[0].Model)
	}
}

// "length" is returned unparsed, with usage, so the engine can bill the
// truncated call and halve.
func TestClassifier_LengthIsNotParsed(t *testing.T) {
	svc := &fakeAI{Output: &ai.GenerateOutput{
		Message: ai.Message{Content: `{"results":[{"ref":1,"sen`}, FinishReason: "length",
		Usage: ai.Usage{PromptTokens: 10, CompletionTokens: 4000},
	}}
	res, err := NewClassifier(svc, "m").Classify(context.Background(), sampleRequest(2))
	if err != nil {
		t.Fatalf("a truncated body must not be an error: %v", err)
	}
	if res.FinishReason != "length" || res.Results != nil || res.CompletionTokens != 4000 {
		t.Fatalf("res = %+v", res)
	}
}

func TestClassifier_ParsesFencedAndPlainJSON(t *testing.T) {
	for _, body := range []string{
		`{"results":[{"ref":1,"sentiment":"positive","stance":"supporter","intent":"praise","topic_key":"saude","is_spam":false,"language":"pt","toxicity":"none","personal_attack":"none","legal_risk":"none"}]}`,
		"```json\n{\"results\":[{\"ref\":1,\"sentiment\":\"positive\",\"stance\":\"supporter\",\"intent\":\"praise\",\"topic_key\":\"saude\",\"is_spam\":false,\"language\":\"pt\",\"toxicity\":\"none\",\"personal_attack\":\"none\",\"legal_risk\":\"none\"}]}\n```",
	} {
		svc := &fakeAI{Output: &ai.GenerateOutput{Message: ai.Message{Content: body}, FinishReason: "stop"}}
		res, err := NewClassifier(svc, "m").Classify(context.Background(), sampleRequest(1))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if len(res.Results) != 1 || res.Results[0].Ref != 1 || res.Results[0].Sentiment != string(shared.SentimentPositive) {
			t.Fatalf("results = %+v", res.Results)
		}
	}
}

// Garbage is an error WITH the usage attached, so the batch is still
// recorded and billed.
func TestClassifier_GarbageIsAnErrorWithUsage(t *testing.T) {
	svc := &fakeAI{Output: &ai.GenerateOutput{Message: ai.Message{Content: "Claro! Aqui está a análise:"}, FinishReason: "stop",
		Usage: ai.Usage{PromptTokens: 50, CompletionTokens: 9}}}
	res, err := NewClassifier(svc, "m").Classify(context.Background(), sampleRequest(1))
	if err == nil {
		t.Fatal("prose is not a classification")
	}
	if res == nil || res.PromptTokens != 50 {
		t.Fatalf("usage must come back with the error: %+v", res)
	}
}

func TestClassifier_ProviderErrorHasNoResult(t *testing.T) {
	svc := &fakeAI{Err: errors.New("502")}
	res, err := NewClassifier(svc, "m").Classify(context.Background(), sampleRequest(1))
	if err == nil || res != nil {
		t.Fatalf("provider error: res=%+v err=%v", res, err)
	}
}
