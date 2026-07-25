package agent

import (
	"encoding/json"
	"strings"
	"time"

	"vozko/domain/agent"
	"vozko/domain/cache"
	"vozko/domain/shared"
)

const agentCacheTTL = 60 * time.Second

type CachedRepository struct {
	inner  agent.Repository
	shared cache.SharedState
	ttl    time.Duration
}

func NewCachedRepository(inner agent.Repository, shared cache.SharedState) agent.Repository {
	return &CachedRepository{inner: inner, shared: shared, ttl: agentCacheTTL}
}

func agentCacheKey(id string) string { return "agent:" + id }

func (r *CachedRepository) invalidate(id string) {
	if r.shared == nil || strings.TrimSpace(id) == "" {
		return
	}
	_ = r.shared.Del(agentCacheKey(id))
}

func (r *CachedRepository) FindByID(id string) (*agent.Agent, error) {
	if strings.TrimSpace(id) == "" {
		return r.inner.FindByID(id)
	}
	if r.shared != nil {
		if raw, err := r.shared.GetString(agentCacheKey(id)); err == nil && raw != "" {
			var a agent.Agent
			if json.Unmarshal([]byte(raw), &a) == nil && a.ID != "" {
				return &a, nil
			}
		}
	}
	a, err := r.inner.FindByID(id)
	if err != nil || a == nil {
		return a, err
	}
	if r.shared != nil {
		if data, mErr := json.Marshal(a); mErr == nil {
			_ = r.shared.SetString(agentCacheKey(id), string(data), r.ttl)
		}
	}
	return a, nil
}

// FindByIDs delegates to the inner repository (batch display lookup, no per-id cache).
func (r *CachedRepository) FindByIDs(agentIDs []string) ([]*agent.Agent, error) {
	return r.inner.FindByIDs(agentIDs)
}

func (r *CachedRepository) Create(a *agent.Agent) error {
	err := r.inner.Create(a)
	if err == nil && a != nil {
		r.invalidate(a.ID)
	}
	return err
}

func (r *CachedRepository) Update(id string, a *agent.Agent) error {
	err := r.inner.Update(id, a)
	if err == nil {
		r.invalidate(id)
	}
	return err
}

func (r *CachedRepository) Delete(id string) error {
	err := r.inner.Delete(id)
	if err == nil {
		r.invalidate(id)
	}
	return err
}

func (r *CachedRepository) List(input agent.ListAgentsInput) (*shared.PaginatedResult[*agent.AgentListItem], error) {
	return r.inner.List(input)
}
