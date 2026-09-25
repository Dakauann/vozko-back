package openrouter

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	openrouter "github.com/revrost/go-openrouter"
)

const (
	openRouterDefaultBaseURL = "https://openrouter.ai/api/v1"
	generationFetchTimeout   = 10 * time.Second
)

var generationRetryDelays = []time.Duration{
	time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second,
}

type generationUsageFetcher interface {
	FetchUsage(ctx context.Context, generationID string) (promptTokens, completionTokens int, costMicros int64, ok bool)
}

type httpGenerationFetcher struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func newHTTPGenerationFetcher(apiKey, baseURL string) *httpGenerationFetcher {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = openRouterDefaultBaseURL
	}
	return &httpGenerationFetcher{
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: base,
		client:  &http.Client{Timeout: generationFetchTimeout},
	}
}

func (f *httpGenerationFetcher) FetchUsage(ctx context.Context, generationID string) (int, int, int64, bool) {
	if f == nil || f.apiKey == "" || strings.TrimSpace(generationID) == "" {
		return 0, 0, 0, false
	}
	endpoint := f.baseURL + "/generation?id=" + url.QueryEscape(generationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, 0, 0, false
	}
	req.Header.Set("Authorization", "Bearer "+f.apiKey)

	resp, err := f.client.Do(req)
	if err != nil {
		log.Printf("[ai-billing] generation usage fetch failed id=%s: %v", generationID, err)
		return 0, 0, 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[ai-billing] generation usage fetch id=%s status=%d", generationID, resp.StatusCode)
		return 0, 0, 0, false
	}

	var payload struct {
		Data struct {
			TokensPrompt     int     `json:"tokens_prompt"`
			TokensCompletion int     `json:"tokens_completion"`
			TotalCost        float64 `json:"total_cost"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		log.Printf("[ai-billing] generation usage decode failed id=%s: %v", generationID, err)
		return 0, 0, 0, false
	}
	return payload.Data.TokensPrompt, payload.Data.TokensCompletion, costToMicros(payload.Data.TotalCost), true
}

func (s *Service) billStreamUsage(workspaceID, model, generationID string, usage *openrouter.Usage) {
	if workspaceID == "" {
		log.Printf("CRITICAL: [ai-billing] missing workspace_id for model=%s, NOT billing (REVENUE LEAK)", model)
		return
	}
	if usage != nil {
		s.publishBillingEvent(workspaceID, model, usage.PromptTokens, usage.CompletionTokens, costToMicros(usage.Cost))
		return
	}
	if s.usageFetcher == nil || strings.TrimSpace(generationID) == "" {
		logUnbilled(workspaceID, model, generationID)
		return
	}
	s.recoveries.Add(1)
	go func() {
		defer s.recoveries.Done()
		s.recoverStreamUsage(workspaceID, model, generationID)
	}()
}

func (s *Service) recoverStreamUsage(workspaceID, model, generationID string) {
	delays := s.usageRetryDelays
	if len(delays) == 0 {
		delays = []time.Duration{0}
	}
	for _, delay := range delays {
		time.Sleep(delay)
		fctx, cancel := context.WithTimeout(context.Background(), generationFetchTimeout)
		pt, ct, costMicros, ok := s.usageFetcher.FetchUsage(fctx, generationID)
		cancel()
		if ok && (pt > 0 || ct > 0 || costMicros > 0) {
			log.Printf("[ai-billing] recovered usage via /generation id=%s model=%s ws=%s prompt=%d completion=%d cost=%dµ",
				generationID, model, workspaceID, pt, ct, costMicros)
			s.publishBillingEvent(workspaceID, model, pt, ct, costMicros)
			return
		}
	}
	logUnbilled(workspaceID, model, generationID)
}

func logUnbilled(workspaceID, model, generationID string) {
	log.Printf("CRITICAL: [ai-billing] no usage for model=%s ws=%s (generation_id=%q, recovery unavailable/empty), NOT billing this stream (REVENUE LEAK)",
		model, workspaceID, generationID)
}
