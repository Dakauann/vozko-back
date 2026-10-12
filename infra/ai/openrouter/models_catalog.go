package openrouter

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	openrouter "github.com/revrost/go-openrouter"

	"vozko/domain/ai"
)

const (
	modelsFetchTimeout         = 10 * time.Second
	modelCatalogTTL            = 30 * time.Minute
	defaultModelSort           = "most-popular"
	catalogOutputModalities    = "text"
	catalogSupportedParameters = "tools"
	imageOutputModalities      = "image"
	audioOutputModalities      = "audio"
	speechOutputModalities     = "speech"
)

func chatModelsQuery() url.Values {
	return url.Values{"output_modalities": {catalogOutputModalities}, "supported_parameters": {catalogSupportedParameters}}
}

func outputModelsQuery(modality string) url.Values {
	return url.Values{"output_modalities": {modality}}
}

type modelCatalogFetcher struct {
	apiKey  string
	baseURL string
	query   url.Values
	client  *http.Client

	mu       sync.Mutex
	cache    []ai.ModelInfo
	cachedAt time.Time
	now      func() time.Time
}

func newModelCatalogFetcher(apiKey, baseURL string) *modelCatalogFetcher {
	return newCatalogFetcher(apiKey, baseURL, chatModelsQuery())
}

func newCatalogFetcher(apiKey, baseURL string, query url.Values) *modelCatalogFetcher {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = openRouterDefaultBaseURL
	}
	return &modelCatalogFetcher{
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: base,
		query:   query,
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
	Architecture struct {
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
	SupportedParameters []string `json:"supported_parameters"`
	Reasoning           *struct {
		SupportedEfforts []string `json:"supported_efforts"`
		DefaultEffort    string   `json:"default_effort"`
		Mandatory        bool     `json:"mandatory"`
	} `json:"reasoning"`
}

func (m openRouterModel) facts() ai.ModelFacts {
	f := ai.ModelFacts{
		ID:               m.ID,
		Name:             m.Name,
		Created:          m.Created,
		InputModalities:  m.Architecture.InputModalities,
		OutputModalities: m.Architecture.OutputModalities,
		Parameters:       m.SupportedParameters,
		ContextLength:    contextLengthOf(m.ContextLength, m.TopProvider.ContextLength),
		Price:            priceOf(m.Pricing.Prompt, m.Pricing.Completion),
	}
	if m.Reasoning != nil {
		f.Reasoning = &ai.ReasoningFacts{Efforts: m.Reasoning.SupportedEfforts, Default: m.Reasoning.DefaultEffort, Mandatory: m.Reasoning.Mandatory}
	}
	return f
}

func sdkModelFacts(m openrouter.Model) ai.ModelFacts {
	return ai.ModelFacts{
		ID:               m.ID,
		Name:             m.Name,
		Created:          m.Created,
		InputModalities:  m.Architecture.InputModalities,
		OutputModalities: m.Architecture.OutputModalities,
		Parameters:       m.SupportedParameters,
		ContextLength:    contextLengthOf(m.ContextLength, m.TopProvider.ContextLength),
		Price:            priceOf(m.Pricing.Prompt, m.Pricing.Completion),
	}
}

func contextLengthOf(model, topProvider *int64) int64 {
	if model != nil && *model > 0 {
		return *model
	}
	if topProvider != nil && *topProvider > 0 {
		return *topProvider
	}
	return 0
}

func priceOf(prompt, completion string) *ai.ModelPrice {
	p, perr := strconv.ParseFloat(strings.TrimSpace(prompt), 64)
	c, cerr := strconv.ParseFloat(strings.TrimSpace(completion), 64)
	if perr != nil || cerr != nil || p < 0 || c < 0 {
		return nil
	}
	return &ai.ModelPrice{PromptPerMillion: p * 1_000_000, CompletionPerMillion: c * 1_000_000}
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

	if fresh, ok := f.fetch(ctx); ok {
		f.mu.Lock()
		f.cache = fresh
		f.cachedAt = f.now()
		f.mu.Unlock()
		return fresh, true
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cache != nil {
		log.Printf("[ai-catalog] refresh failed, serving the catalog from %s", f.cachedAt.Format(time.RFC3339))
		return f.cache, true
	}
	return nil, false
}

func (f *modelCatalogFetcher) fetch(ctx context.Context) ([]ai.ModelInfo, bool) {
	params := url.Values{}
	for key, values := range f.query {
		params[key] = values
	}
	params.Set("sort", defaultModelSort)
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
		result = append(result, ai.DescribeModel(m.facts()))
	}
	return result, true
}
