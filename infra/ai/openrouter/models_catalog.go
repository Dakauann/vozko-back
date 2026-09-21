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
	modelsFetchTimeout         = 10 * time.Second
	modelCatalogTTL            = 30 * time.Minute
	defaultModelSort           = "most-popular"
	catalogOutputModalities    = "text"
	catalogSupportedParameters = "tools"
)

type modelCatalogFetcher struct {
	apiKey  string
	baseURL string
	client  *http.Client

	mu       sync.Mutex
	cache    []ai.ModelInfo
	cachedAt time.Time
	now      func() time.Time
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
