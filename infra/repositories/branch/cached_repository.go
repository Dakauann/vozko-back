package branch_repository

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"vozko/domain/branch"
	"vozko/domain/cache"
)

const (
	// branchDefCacheTTL is the cache-aside lifetime for a branch definition. Kept short
	// so a change that somehow bypasses invalidation (e.g. the bulk boot reset)
	// self-heals quickly; invalidate-on-write keeps the common case immediately
	// consistent.
	branchDefCacheTTL = 60 * time.Second
	// negativeCacheTTL briefly caches a "no such branch" result so a repeated
	// unknown-user REGISTER cannot hit Postgres on every attempt (the DB-amplification
	// flood vector). Any Create/Update of that sip_user invalidates the entry.
	negativeCacheTTL = 30 * time.Second
	// branchNotFoundMarker is the sentinel stored for a negative-cache hit. It is not
	// valid JSON for cachedBranchDTO, so it can never be mistaken for a real branch.
	branchNotFoundMarker = "\x00notfound"
)

// CachedRepository is a cache-aside decorator over the branch Repository. It exists for
// ONE hot path: the SIP registrar resolves FindByGlobalSIPUser on every REGISTER,
// including the refresh every <=120s per phone, so at scale that is a steady stream of
// identical Postgres reads. Branch definitions ARE durable Postgres rows (unlike the
// ephemeral AOR bindings, which are Redis-primary), so cache-aside is the correct
// pattern here: read-through on a miss, write-through invalidation on every mutation.
//
// Correctness note: the register use case reads RegistrationStatus off the branch to
// suppress a redundant status write, so the cache MUST invalidate on
// UpdateRegistrationStatus (and the bulk reset) or it would fight that optimization and
// re-write the status on every refresh. Every write path below invalidates.
type CachedRepository struct {
	inner  branch.Repository
	shared cache.SharedState
	ttl    time.Duration
}

func NewCachedRepository(inner branch.Repository, shared cache.SharedState) branch.Repository {
	return &CachedRepository{inner: inner, shared: shared, ttl: branchDefCacheTTL}
}

func branchSIPKey(sipUser string) string {
	return "branch:def:sip:" + strings.ToLower(strings.TrimSpace(sipUser))
}

// cachedBranchDTO stores the branch plus its HA1 secret separately, because
// Branch.SecretHA1 is json:"-" (never serialized) yet the registrar needs it to verify
// the digest on a cache hit. This mirrors how the business-phone cache carries the
// Dialog360/Meta credential; it is a short-lived 60s Redis copy of a value that is
// encrypted at rest in Postgres.
type cachedBranchDTO struct {
	Branch    branch.Branch `json:"b"`
	SecretHA1 string        `json:"ha1"`
}

func (r *CachedRepository) writeCache(b *branch.Branch) {
	if r.shared == nil || b == nil || b.SIPUser == "" {
		return
	}
	raw, err := json.Marshal(cachedBranchDTO{Branch: *b, SecretHA1: b.SecretHA1})
	if err != nil {
		return
	}
	_ = r.shared.SetString(branchSIPKey(b.SIPUser), string(raw), r.ttl)
}

func (r *CachedRepository) readRaw(sipUser string) string {
	if r.shared == nil {
		return ""
	}
	raw, err := r.shared.GetString(branchSIPKey(sipUser))
	if err != nil {
		return ""
	}
	return raw
}

func decodeCachedBranch(raw string) *branch.Branch {
	var dto cachedBranchDTO
	if json.Unmarshal([]byte(raw), &dto) != nil || dto.Branch.ID == "" {
		return nil
	}
	b := dto.Branch
	b.SecretHA1 = dto.SecretHA1
	return &b
}

// invalidateSIP drops the cache for a sip_user. Given only an id, it resolves the
// sip_user from the inner repo first (rare paths: status transitions, delete).
func (r *CachedRepository) invalidateSIP(sipUser string) {
	if r.shared == nil || strings.TrimSpace(sipUser) == "" {
		return
	}
	_ = r.shared.Del(branchSIPKey(sipUser))
}

func (r *CachedRepository) invalidateByID(id string) {
	if r.shared == nil || strings.TrimSpace(id) == "" {
		return
	}
	if existing, err := r.inner.FindByID(id); err == nil && existing != nil {
		r.invalidateSIP(existing.SIPUser)
	}
}

// FindByGlobalSIPUser is the cached hot path.
func (r *CachedRepository) FindByGlobalSIPUser(sipUser string) (*branch.Branch, error) {
	if strings.TrimSpace(sipUser) == "" {
		return r.inner.FindByGlobalSIPUser(sipUser)
	}
	switch raw := r.readRaw(sipUser); {
	case raw == branchNotFoundMarker:
		return nil, branch.ErrBranchNotFound // negative-cache hit: never touches the DB
	case raw != "":
		if b := decodeCachedBranch(raw); b != nil {
			return b, nil
		}
	}
	b, err := r.inner.FindByGlobalSIPUser(sipUser)
	if err != nil {
		// Negative-cache a genuine not-found so a repeated unknown-user probe stops
		// hammering Postgres. Ambiguous (SIPUserTaken) is a data issue, not cached.
		if errors.Is(err, branch.ErrBranchNotFound) && r.shared != nil {
			_ = r.shared.SetString(branchSIPKey(sipUser), branchNotFoundMarker, negativeCacheTTL)
		}
		return b, err
	}
	if b == nil {
		return b, err
	}
	r.writeCache(b)
	return b, nil
}

func (r *CachedRepository) Create(b *branch.Branch) error {
	err := r.inner.Create(b)
	if err == nil && b != nil {
		r.invalidateSIP(b.SIPUser)
	}
	return err
}

func (r *CachedRepository) Update(b *branch.Branch) error {
	err := r.inner.Update(b)
	if err == nil && b != nil {
		r.invalidateSIP(b.SIPUser)
	}
	return err
}

func (r *CachedRepository) Delete(id string) error {
	r.invalidateByID(id) // resolve sip_user before the row is gone
	return r.inner.Delete(id)
}

func (r *CachedRepository) UpdateRegistrationStatus(id string, status branch.RegistrationStatus) error {
	err := r.inner.UpdateRegistrationStatus(id, status)
	if err == nil {
		r.invalidateByID(id)
	}
	return err
}

func (r *CachedRepository) ResetLiveRegistrations() (int64, error) {
	n, err := r.inner.ResetLiveRegistrations()
	// A bulk status change: rather than track every affected key, let the short TTL
	// heal stale entries (this runs once on boot; live branches are re-stamped and thus
	// invalidated by rehydration's UpdateRegistrationStatus calls anyway).
	return n, err
}

// --- pass-through reads (not on a hot path) ------------------------------

func (r *CachedRepository) FindByID(id string) (*branch.Branch, error) { return r.inner.FindByID(id) }
func (r *CachedRepository) FindBySIPUser(workspaceID, sipUser string) (*branch.Branch, error) {
	return r.inner.FindBySIPUser(workspaceID, sipUser)
}
func (r *CachedRepository) FindByWorkspace(workspaceID string, page, pageSize int) ([]*branch.Branch, int64, error) {
	return r.inner.FindByWorkspace(workspaceID, page, pageSize)
}
func (r *CachedRepository) FindByUser(workspaceID, userID string) ([]*branch.Branch, error) {
	return r.inner.FindByUser(workspaceID, userID)
}
func (r *CachedRepository) CountByWorkspace(workspaceID string) (int64, error) {
	return r.inner.CountByWorkspace(workspaceID)
}
