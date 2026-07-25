package openrouter

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// ModelValidator answers "is this an exact model id the provider offers?" against
// a TTL-cached snapshot of the catalog — the same caching discipline as
// LLMPriceFetcher (refresh when stale, fall back to a stale snapshot on error).
//
// Callers validate only the handful of model ids actually used (a cheap map
// lookup), instead of each caller fetching and holding the whole ~300-model
// catalog. The catalog is fetched at most once per TTL window, process-wide.
type ModelValidator struct {
	svc      *Service
	cacheTTL time.Duration

	mu        sync.RWMutex
	ids       map[string]struct{}
	fetchedAt time.Time
}

func NewModelValidator(svc *Service, ttl time.Duration) *ModelValidator {
	return &ModelValidator{svc: svc, cacheTTL: ttl, ids: make(map[string]struct{})}
}

// IsValidModel reports whether id is an exact model id the provider offers. It
// returns an error only when the catalog has never been loaded and a refresh
// fails — callers should treat that as "unknown" and NOT block on it.
func (v *ModelValidator) IsValidModel(ctx context.Context, id string) (bool, error) {
	v.mu.RLock()
	stale := time.Since(v.fetchedAt) > v.cacheTTL || len(v.ids) == 0
	v.mu.RUnlock()

	if stale {
		if err := v.refresh(ctx); err != nil {
			v.mu.RLock()
			hasData := len(v.ids) > 0
			v.mu.RUnlock()
			if !hasData {
				return false, fmt.Errorf("model validator: %w", err)
			}
			log.Printf("[model-validator] refresh failed, using stale cache: %v", err)
		}
	}

	v.mu.RLock()
	defer v.mu.RUnlock()
	_, ok := v.ids[id]
	return ok, nil
}

func (v *ModelValidator) refresh(ctx context.Context) error {
	models, err := v.svc.GetAvaibleModels(ctx)
	if err != nil {
		return err
	}
	if len(models) == 0 {
		return fmt.Errorf("provider returned no models")
	}
	ids := make(map[string]struct{}, len(models))
	for _, m := range models {
		ids[m] = struct{}{}
	}
	v.mu.Lock()
	v.ids = ids
	v.fetchedAt = time.Now()
	v.mu.Unlock()
	log.Printf("[model-validator] refreshed: %d model ids cached", len(ids))
	return nil
}
