package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/tools"
)

const reasoningCatalogBody = `{"data":[
  {"id":"lab/thinker","name":"Thinker","supported_parameters":["tools","reasoning"],
   "reasoning":{"mandatory":false,"supported_efforts":["high","medium","low","none"],"default_effort":"medium"}},
  {"id":"lab/plain","name":"Plain","supported_parameters":["tools"]}
]}`

func reasoningService(t *testing.T) *Service {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(reasoningCatalogBody))
	}))
	t.Cleanup(srv.Close)
	s := mustService(t, Config{DefaultModel: "lab/plain"}, injectFakeToolService{})
	s.catalogFetcher = newModelCatalogFetcher("test-key", srv.URL)
	return s
}

func reasoningInput(level ai.ReasoningLevel, budget int) ai.GenerateInput {
	return ai.GenerateInput{
		Model:              "lab/thinker",
		SystemPrompt:       "regras fixas",
		Messages:           []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
		Tools:              []tools.Definition{{Name: "b_tool", Description: "b"}, {Name: "a_tool", Description: "a"}},
		ReasoningMaxTokens: budget,
		ReasoningEffort:    level,
	}
}

func TestBuildRequest_MapsTheChosenLevelToTheEffortField(t *testing.T) {
	s := reasoningService(t)
	req := s.buildRequest(reasoningInput(ai.ReasoningHigh, 10000))
	if req.Reasoning == nil || req.Reasoning.Effort == nil || *req.Reasoning.Effort != "high" {
		t.Fatalf("reasoning = %+v, want effort high", req.Reasoning)
	}
	if req.Reasoning.MaxTokens != nil {
		t.Fatalf("max_tokens = %d, want unset next to an effort", *req.Reasoning.MaxTokens)
	}
}

func TestBuildRequest_NoLevelKeepsTheTokenBudget(t *testing.T) {
	s := reasoningService(t)
	req := s.buildRequest(reasoningInput("", 5000))
	if req.Reasoning == nil || req.Reasoning.Effort != nil || req.Reasoning.MaxTokens == nil || *req.Reasoning.MaxTokens != 5000 {
		t.Fatalf("reasoning = %+v, want only max_tokens 5000", req.Reasoning)
	}
	if bare := s.buildRequest(reasoningInput("", 0)); bare.Reasoning != nil {
		t.Fatalf("reasoning = %+v, want none without a level or budget", bare.Reasoning)
	}
}

func TestBuildRequest_TheLevelLeavesThePromptPrefixAndToolsUntouched(t *testing.T) {
	s := reasoningService(t)
	low := s.buildRequest(reasoningInput(ai.ReasoningLow, 10000))
	high := s.buildRequest(reasoningInput(ai.ReasoningHigh, 10000))
	lowMsgs, _ := json.Marshal(low.Messages)
	highMsgs, _ := json.Marshal(high.Messages)
	lowTools, _ := json.Marshal(low.Tools)
	highTools, _ := json.Marshal(high.Tools)
	if string(lowMsgs) != string(highMsgs) || string(lowTools) != string(highTools) {
		t.Fatal("changing the reasoning level changed the messages or the tools")
	}
}

func TestGenerate_RefusesALevelTheModelDoesNotOffer(t *testing.T) {
	s := reasoningService(t)
	for _, in := range []ai.GenerateInput{
		reasoningInput(ai.ReasoningMax, 0),
		func() ai.GenerateInput { i := reasoningInput(ai.ReasoningLow, 0); i.Model = "lab/plain"; return i }(),
		func() ai.GenerateInput { i := reasoningInput(ai.ReasoningLow, 0); i.Model = "lab/unknown"; return i }(),
	} {
		if _, err := s.Generate(context.Background(), in); !errors.Is(err, ai.ErrReasoningLevelUnsupported) {
			t.Fatalf("Generate(%s, %s) err = %v, want refusal", in.Model, in.ReasoningEffort, err)
		}
		if _, err := s.GenerateStream(context.Background(), in); !errors.Is(err, ai.ErrReasoningLevelUnsupported) {
			t.Fatalf("GenerateStream(%s, %s) err = %v, want refusal", in.Model, in.ReasoningEffort, err)
		}
	}
}

func TestGenerate_RefusesALevelWhenTheCatalogIsUnavailable(t *testing.T) {
	s := mustService(t, Config{DefaultModel: "lab/plain"}, injectFakeToolService{})
	s.catalogFetcher = newModelCatalogFetcher("", openRouterDefaultBaseURL)
	if _, err := s.Generate(context.Background(), reasoningInput(ai.ReasoningHigh, 0)); !errors.Is(err, ai.ErrReasoningLevelUnsupported) {
		t.Fatalf("err = %v, want refusal without a catalog", err)
	}
}
