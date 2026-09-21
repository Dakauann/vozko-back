package openrouter

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

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
