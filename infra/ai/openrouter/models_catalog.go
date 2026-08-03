package openrouter

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"vozko/domain/ai"
)

const (
	modelsFetchTimeout = 10 * time.Second
	// modelCatalogTTL caps how often we hit OpenRouter's /models endpoint for the
	// display catalog. The list changes rarely, so serving it live on every
	// /agents/options call only hammered OpenRouter for no benefit.
	modelCatalogTTL = 30 * time.Minute
	// defaultModelSort orders the catalog by OpenRouter's own popularity ranking so
	// the most-used models surface first in the picker. The go-openrouter client's
	// ListModels can't pass this (it sends no query params), which is why we fetch
	// /models directly here.
	defaultModelSort = "most-popular"
	// catalogOutputModalities restricts the catalog to text-generating (chat) models
	// via OpenRouter's documented output_modalities filter, dropping image-gen,
	// embeddings, audio/TTS, and rerankers that don't belong in an LLM picker. This
	// is an official server-side filter, see
	// https://openrouter.ai/docs/api/api-reference/models/get-models
	catalogOutputModalities = "text"
	// catalogSupportedParameters restricts the catalog to tool-capable models via
	// OpenRouter's documented supported_parameters filter. Every selectable model
	// slot on the platform (voice/WhatsApp agents, the simulator, analysis, workflow
	// AI-agent nodes, and the AI workflow builder) calls the model with function
	// calling, so a model without tool support can't work as an agent here.
	catalogSupportedParameters = "tools"
)

// modelCatalogFetcher loads the priced model catalog directly from OpenRouter's
// GET /models endpoint. It exists because the go-openrouter client's ListModels
// sends no query params, so it can't request server-side sorting (?sort=...) and
// drops fields we surface in the picker UI (created, context_length). Results are
// cached for modelCatalogTTL. Optional on the Service: when nil the caller falls
// back to the unsorted library path.
type modelCatalogFetcher struct {
	apiKey  string
	baseURL string
	client  *http.Client

	mu       sync.Mutex
	cache    []ai.ModelInfo
	cachedAt time.Time
	now      func() time.Time // injectable for tests
}

func newModelCatalogFetcher(apiKey, baseURL string) *modelCatalogFetcher {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = openRouterDefaultBaseURL
	}
	return &modelCatalogFetcher{
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: base,
		client:  &http.Client{Timeout: modelsFetchTimeout},
		now:     time.Now,
	}
}

// openRouterModel is the subset of the /models response we care about. OpenRouter
// reports context_length both at the top level and under top_provider; we read the
// top level first and fall back to the provider value.
type openRouterModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Created       int64  `json:"created"`
	ContextLength *int64 `json:"context_length"`
	Pricing       struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
	} `json:"pricing"`
	TopProvider struct {
		ContextLength *int64 `json:"context_length"`
	} `json:"top_provider"`
}

// FetchModelsWithPricing returns the catalog in OpenRouter's most-popular order,
// enriched with pricing (scaled to micros per million tokens), creation time, and
// context length. ok is false on any error so the caller can fall back to the
// library path.
func (f *modelCatalogFetcher) FetchModelsWithPricing(ctx context.Context) ([]ai.ModelInfo, bool) {
	if f == nil || f.apiKey == "" {
		return nil, false
	}

	f.mu.Lock()
	if f.cache != nil && f.now().Sub(f.cachedAt) < modelCatalogTTL {
		cached := f.cache
		f.mu.Unlock()
		return cached, true
	}
	f.mu.Unlock()

	// Official OpenRouter /models query params only, no client-side filtering.
	params := url.Values{}
	params.Set("sort", defaultModelSort)
	params.Set("output_modalities", catalogOutputModalities)
	params.Set("supported_parameters", catalogSupportedParameters)
	endpoint := f.baseURL + "/models?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+f.apiKey)

	resp, err := f.client.Do(req)
	if err != nil {
		log.Printf("[ai-catalog] models fetch failed: %v", err)
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[ai-catalog] models fetch status=%d", resp.StatusCode)
		return nil, false
	}

	var payload struct {
		Data []openRouterModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		log.Printf("[ai-catalog] models decode failed: %v", err)
		return nil, false
	}

	result := make([]ai.ModelInfo, 0, len(payload.Data))
	for _, m := range payload.Data {
		info := ai.ModelInfo{
			ID:      m.ID,
			Name:    m.Name,
			Created: m.Created,
		}
		if v := parseFloat64(m.Pricing.Prompt); v > 0 {
			info.PromptPrice = v * 1_000_000
		}
		if v := parseFloat64(m.Pricing.Completion); v > 0 {
			info.CompletionPrice = v * 1_000_000
		}
		if m.ContextLength != nil && *m.ContextLength > 0 {
			info.ContextLength = *m.ContextLength
		} else if m.TopProvider.ContextLength != nil && *m.TopProvider.ContextLength > 0 {
			info.ContextLength = *m.TopProvider.ContextLength
		}
		result = append(result, info)
	}

	f.mu.Lock()
	f.cache = result
	f.cachedAt = f.now()
	f.mu.Unlock()

	return result, true
}
