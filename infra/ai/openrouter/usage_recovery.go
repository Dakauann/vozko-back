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

// generationUsageFetcher recovers token usage for a generation whose inline
// stream usage chunk never arrived, i.e. the stream was cancelled, timed out, or
// aborted before OpenRouter sent the final usage chunk (which carries the token
// counts). It queries OpenRouter's GET /generation endpoint by id. Optional on
// the Service: when nil, recovery is disabled and a cut stream simply isn't
// billed (the legacy behaviour, a revenue leak).
type generationUsageFetcher interface {
	FetchUsage(ctx context.Context, generationID string) (promptTokens, completionTokens int, ok bool)
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

// FetchUsage calls GET {baseURL}/generation?id=<id> and returns the OpenRouter
// normalized prompt/completion token counts (the same basis as the inline stream
// usage and the per-token pricing). ok is false on any error or non-200 so the
// caller can fall back to the leak log rather than billing a wrong amount.
func (f *httpGenerationFetcher) FetchUsage(ctx context.Context, generationID string) (int, int, bool) {
	if f == nil || f.apiKey == "" || strings.TrimSpace(generationID) == "" {
		return 0, 0, false
	}
	endpoint := f.baseURL + "/generation?id=" + url.QueryEscape(generationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, 0, false
	}
	req.Header.Set("Authorization", "Bearer "+f.apiKey)

	resp, err := f.client.Do(req)
	if err != nil {
		log.Printf("[ai-billing] generation usage fetch failed id=%s: %v", generationID, err)
		return 0, 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[ai-billing] generation usage fetch id=%s status=%d", generationID, resp.StatusCode)
		return 0, 0, false
	}

	var payload struct {
		Data struct {
			TokensPrompt     int `json:"tokens_prompt"`
			TokensCompletion int `json:"tokens_completion"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		log.Printf("[ai-billing] generation usage decode failed id=%s: %v", generationID, err)
		return 0, 0, false
	}
	return payload.Data.TokensPrompt, payload.Data.TokensCompletion, true
}

// billStreamUsage bills a single streamed turn. With an inline usage chunk it
// bills directly. When that chunk never arrived (the stream was cancelled, timed
// out, or aborted), it recovers the real usage from OpenRouter's /generation
// endpoint by the generation id captured from the stream, so the turn is still
// charged. Only when recovery is unavailable or returns nothing does it fall back
// to logging the (now-rare) revenue leak.
func (s *Service) billStreamUsage(workspaceID, model, generationID string, usage *openrouter.Usage) {
	if workspaceID == "" {
		log.Printf("CRITICAL: [ai-billing] missing workspace_id for model=%s, NOT billing (REVENUE LEAK)", model)
		return
	}
	if usage != nil {
		s.publishBillingEvent(workspaceID, model, usage.PromptTokens, usage.CompletionTokens)
		return
	}
	if s.usageFetcher != nil && strings.TrimSpace(generationID) != "" {
		fctx, cancel := context.WithTimeout(context.Background(), generationFetchTimeout)
		pt, ct, ok := s.usageFetcher.FetchUsage(fctx, generationID)
		cancel()
		if ok && (pt > 0 || ct > 0) {
			log.Printf("[ai-billing] recovered usage for cut stream via /generation id=%s model=%s ws=%s prompt=%d completion=%d",
				generationID, model, workspaceID, pt, ct)
			s.publishBillingEvent(workspaceID, model, pt, ct)
			return
		}
	}
	log.Printf("CRITICAL: [ai-billing] no usage for model=%s ws=%s (generation_id=%q, recovery unavailable/empty), NOT billing this stream (REVENUE LEAK)",
		model, workspaceID, generationID)
}
