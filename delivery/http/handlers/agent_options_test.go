package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/ai"
)

type optionsAI struct {
	ai.Service
	models []ai.ModelInfo
}

func (o optionsAI) GetModelsWithPricing(context.Context) ([]ai.ModelInfo, error) {
	return o.models, nil
}

func TestListOptionsCarriesCapabilitiesAndReasoningLevels(t *testing.T) {
	thinker := ai.DescribeModel(ai.ModelFacts{
		ID:              "lab/thinker",
		InputModalities: []string{"text", "image"},
		Parameters:      []string{"tools", "reasoning"},
		Reasoning:       &ai.ReasoningFacts{Efforts: []string{"high", "low"}, Default: "low"},
	})
	h := &AgentHandler{aiService: optionsAI{models: []ai.ModelInfo{thinker}}}
	rec := httptest.NewRecorder()
	h.ListOptions(rec, httptest.NewRequest(http.MethodGet, "/agents/options", nil))

	var body agentOptionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if len(body.ModelCapabilities) != len(ai.ModelCapabilities()) || body.ModelCapabilities[0] != ai.CapabilityVision {
		t.Fatalf("modelCapabilities = %v, want the shared vocabulary", body.ModelCapabilities)
	}
	if len(body.ModelPricing) != 1 {
		t.Fatalf("modelPricing = %+v", body.ModelPricing)
	}
	got := body.ModelPricing[0]
	if !got.Has(ai.CapabilityVision) || !got.Has(ai.CapabilityTools) || got.Reasoning == nil || got.Reasoning.Default != ai.ReasoningLow {
		t.Fatalf("model = %+v, want vision, tools and the low default", got)
	}
}
