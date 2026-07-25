package openrouter

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"vozko/domain/workspace/workspace_pricing"
)

type llmPriceEntry struct {
	InputMicros  int64
	OutputMicros int64
}

type LLMPriceFetcher struct {
	svc      *Service
	cacheTTL time.Duration

	mu        sync.RWMutex
	cache     map[string]llmPriceEntry
	fetchedAt time.Time
}

var _ workspace_pricing.LLMPriceFetcher = (*LLMPriceFetcher)(nil)

func NewLLMPriceFetcher(svc *Service, ttl time.Duration) *LLMPriceFetcher {
	return &LLMPriceFetcher{
		svc:      svc,
		cacheTTL: ttl,
		cache:    make(map[string]llmPriceEntry),
	}
}

func (f *LLMPriceFetcher) FetchLLMPriceMicros(model string) (inputMicros, outputMicros int64, err error) {
	f.mu.RLock()
	stale := time.Since(f.fetchedAt) > f.cacheTTL || len(f.cache) == 0
	f.mu.RUnlock()

	if stale {
		if err := f.refresh(); err != nil {

			f.mu.RLock()
			hasData := len(f.cache) > 0
			f.mu.RUnlock()
			if !hasData {
				return 0, 0, fmt.Errorf("llm price fetcher: %w", err)
			}
			log.Printf("[llm-pricer] refresh failed, using stale cache: %v", err)
		}
	}

	f.mu.RLock()
	defer f.mu.RUnlock()

	if e, ok := f.cache[model]; ok {
		return e.InputMicros, e.OutputMicros, nil
	}

	normalized := model
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		normalized = model[idx+1:]
	}
	for k, e := range f.cache {
		suffix := k
		if idx := strings.LastIndex(k, "/"); idx >= 0 {
			suffix = k[idx+1:]
		}
		if suffix == normalized {
			return e.InputMicros, e.OutputMicros, nil
		}
	}

	return 0, 0, nil
}

func (f *LLMPriceFetcher) refresh() error {
	models, err := f.svc.client.ListModels(context.Background())
	if err != nil {
		return err
	}

	cache := make(map[string]llmPriceEntry, len(models))
	for _, m := range models {
		promptPerToken := parseFloat64(m.Pricing.Prompt)
		completionPerToken := parseFloat64(m.Pricing.Completion)

		if promptPerToken <= 0 && completionPerToken <= 0 {
			continue
		}

		cache[m.ID] = llmPriceEntry{
			InputMicros:  int64(math.Ceil(promptPerToken * 1_000_000 * 1_000_000)),
			OutputMicros: int64(math.Ceil(completionPerToken * 1_000_000 * 1_000_000)),
		}
	}

	f.mu.Lock()
	f.cache = cache
	f.fetchedAt = time.Now()
	f.mu.Unlock()

	log.Printf("[llm-pricer] refreshed OpenRouter model prices: %d models cached", len(cache))
	return nil
}
