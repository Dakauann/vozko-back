package openrouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	openrouter "github.com/revrost/go-openrouter"

	"vozko/domain/ai"
)

const catalogShapeBody = `{
  "data": [
    {
      "id": "lab/vision-thinker",
      "canonical_slug": "lab/vision-thinker-20261001",
      "hugging_face_id": null,
      "name": "Vision Thinker",
      "created": 1790000000,
      "description": "x",
      "context_length": 1000000,
      "architecture": {
        "modality": "text+image+audio->text",
        "input_modalities": ["text", "image", "audio", "video"],
        "output_modalities": ["text"],
        "tokenizer": "Other",
        "instruct_type": null
      },
      "pricing": {
        "prompt": "0.0000001",
        "completion": "0.0000005",
        "input_cache_read": "0.00000001",
        "overrides": [{"min_prompt_tokens": 100000, "prompt": "0.0000005"}]
      },
      "top_provider": {"context_length": 1000000, "max_completion_tokens": 64000, "is_moderated": false},
      "per_request_limits": null,
      "supported_parameters": ["include_reasoning", "max_tokens", "reasoning", "temperature", "tool_choice", "tools"],
      "default_parameters": {"temperature": null},
      "supported_voices": null,
      "knowledge_cutoff": null,
      "expiration_date": null,
      "links": {"details": "/api/v1/models/lab/vision-thinker/endpoints"},
      "benchmarks": {"artificial_analysis": {"intelligence_index": 43.4, "coding_index": null}},
      "reasoning": {"mandatory": true, "supported_efforts": ["max", "high", "medium", "low", "none", "galaxy"], "default_effort": "medium", "default_enabled": true},
      "future_field": {"anything": [1, 2, 3]}
    },
    {
      "id": "lab/painter:free",
      "name": "Painter",
      "created": 1780000000,
      "context_length": 32768,
      "architecture": {"input_modalities": ["text"], "output_modalities": ["image", "text"]},
      "pricing": {"prompt": "0", "completion": "0"},
      "supported_parameters": ["tools", "reasoning"],
      "reasoning": {"mandatory": false}
    },
    {
      "id": "lab/mystery",
      "name": "Mystery",
      "pricing": {"prompt": "abc", "completion": "0"},
      "reasoning": {"supported_efforts": null, "default_effort": "medium"}
    },
    {
      "id": "openrouter/auto",
      "name": "Auto Router",
      "pricing": {"prompt": "-1", "completion": "-1"},
      "supported_parameters": ["tools"]
    }
  ]
}`

func fetchCatalogShape(t *testing.T) []ai.ModelInfo {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(catalogShapeBody))
	}))
	t.Cleanup(srv.Close)
	models, ok := newModelCatalogFetcher("k", srv.URL).FetchModelsWithPricing(context.Background())
	if !ok || len(models) != 4 {
		t.Fatalf("fetch ok=%v len=%d, want 4 models", ok, len(models))
	}
	return models
}

func TestModelCatalog_DerivesCapabilitiesFromTheCatalogShape(t *testing.T) {
	models := fetchCatalogShape(t)
	cases := []struct {
		id   string
		want []ai.ModelCapability
	}{
		{id: "lab/vision-thinker", want: []ai.ModelCapability{ai.CapabilityVision, ai.CapabilityTools, ai.CapabilityReasoning, ai.CapabilityLongContext, ai.CapabilityAudioInput, ai.CapabilityLowPrice}},
		{id: "lab/painter:free", want: []ai.ModelCapability{ai.CapabilityTools, ai.CapabilityReasoning, ai.CapabilityImageOutput, ai.CapabilityLowPrice, ai.CapabilityFree}},
		{id: "lab/mystery", want: []ai.ModelCapability{}},
		{id: "openrouter/auto", want: []ai.ModelCapability{ai.CapabilityTools}},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			m, ok := ai.FindModel(models, tc.id)
			if !ok {
				t.Fatalf("%s missing", tc.id)
			}
			if !slices.Equal(m.Capabilities, tc.want) {
				t.Fatalf("capabilities = %v, want %v", m.Capabilities, tc.want)
			}
		})
	}
}

func TestModelCatalog_ReadsReasoningLevelsFromTheCatalog(t *testing.T) {
	models := fetchCatalogShape(t)
	thinker, _ := ai.FindModel(models, "lab/vision-thinker")
	if thinker.Reasoning == nil {
		t.Fatal("reasoning = nil, want the catalog efforts")
	}
	want := []ai.ReasoningLevel{ai.ReasoningLow, ai.ReasoningMedium, ai.ReasoningHigh, ai.ReasoningMax}
	if !slices.Equal(thinker.Reasoning.Levels, want) || thinker.Reasoning.Default != ai.ReasoningMedium {
		t.Fatalf("reasoning = %+v, want %v with medium", thinker.Reasoning, want)
	}
	for _, id := range []string{"lab/painter:free", "lab/mystery", "openrouter/auto"} {
		m, _ := ai.FindModel(models, id)
		if m.Reasoning != nil {
			t.Errorf("%s reasoning = %+v, want none offered", id, m.Reasoning)
		}
	}
}

func TestModelCatalog_UnknownPricesStayUnknown(t *testing.T) {
	models := fetchCatalogShape(t)
	for _, id := range []string{"lab/mystery", "openrouter/auto"} {
		m, _ := ai.FindModel(models, id)
		if m.PromptPrice != 0 || m.CompletionPrice != 0 || m.Has(ai.CapabilityFree) {
			t.Errorf("%s = %+v, want unknown prices that are never free", id, m)
		}
	}
}

func TestModelCatalog_KeepsTheLastGoodCatalogWhenARefreshFails(t *testing.T) {
	var failing atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(catalogShapeBody))
	}))
	defer srv.Close()

	base := time.Unix(1_700_000_000, 0)
	f := newModelCatalogFetcher("k", srv.URL)
	f.now = func() time.Time { return base }
	if _, ok := f.FetchModelsWithPricing(context.Background()); !ok {
		t.Fatal("first fetch failed")
	}
	failing.Store(true)
	f.now = func() time.Time { return base.Add(modelCatalogTTL + time.Minute) }
	models, ok := f.FetchModelsWithPricing(context.Background())
	if !ok || len(models) != 4 {
		t.Fatalf("stale fetch ok=%v len=%d, want the last good catalog", ok, len(models))
	}
}

func sdkModelFixture(id string, contextLength int64, inputs, params []string, prompt, completion string) openrouter.Model {
	return openrouter.Model{
		ID:                  id,
		Name:                id,
		ContextLength:       &contextLength,
		Architecture:        openrouter.ModelArchitecture{InputModalities: inputs, OutputModalities: []string{"text"}},
		Pricing:             openrouter.ModelPricing{Prompt: prompt, Completion: completion},
		SupportedParameters: params,
	}
}

func TestDescribeSDKModel_UsesTheSameRule(t *testing.T) {
	ctx := int64(2_000_000)
	m := sdkModelFacts(sdkModelFixture("lab/sdk", ctx, []string{"text", "image"}, []string{"tools"}, "0.000001", "0.000002"))
	info := ai.DescribeModel(m)
	want := []ai.ModelCapability{ai.CapabilityVision, ai.CapabilityTools, ai.CapabilityLongContext}
	if !slices.Equal(info.Capabilities, want) || info.PromptPrice != 1 || info.CompletionPrice != 2 {
		t.Fatalf("sdk model = %+v, want %v at 1 / 2", info, want)
	}
	if info.Reasoning != nil {
		t.Fatal("the SDK listing carries no effort levels, so none may be offered")
	}
}
