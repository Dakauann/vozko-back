package branchinfra

import (
	"encoding/json"
	"fmt"
	"time"

	"vozko/domain/branch"
	domainCache "vozko/domain/cache"
)

// redisBindingStore is the durable, shared AOR location service (the Kamailio/OpenSIPS
// usrloc equivalent) backed by the process-external Redis. Making Redis the system of
// record for live bindings (NOT a cache over Postgres: there is no authoritative row to
// revalidate against, a miss simply means "not registered") buys two things the
// in-process store could not:
//
//   - Durability across an app restart: Redis is a separate process, so the bindings
//     survive a Vozko restart. The registrar rehydrates its routing sessions from them
//     on boot, so a restart has no "phone thinks it is registered but we forgot" gap.
//   - Shared presence across replicas: every replica sees the same bindings.
//
// Key layout (all TTL-guarded so a dead replica's rows self-clean):
//
//	branch:reg:bind:{sip}:{callID}  -> JSON(RegistrationBinding)   [SET EX backstopTTL]
//	branch:reg:aor:{sip}            -> SET of callIDs (contact index)
//	branch:reg:aors                -> SET of sip_users with live contacts (rehydrate index)
//
// Logical expiry is enforced by the binding's ExpiresAt (checked in ListLive/ReapExpired)
// so ReapExpired keeps the SAME contract as the in-memory store: it returns the evicted
// bindings, which is what drives the presence-offline transition (OnBranchUnreachable +
// dashboard status). The Redis TTL is only a generous backstop for a replica that dies
// without reaping.
type redisBindingStore struct {
	rs          domainCache.SharedState
	now         func() time.Time
	backstopTTL time.Duration
}

// NewRedisBindingStore builds a Redis-backed binding store. backstopTTL should be
// comfortably larger than the max registration expiry AND the reaper interval, so the
// reaper always catches logical expiry (and can return the evicted binding) before Redis
// drops the key itself.
func NewRedisBindingStore(rs domainCache.SharedState, now func() time.Time) branch.BindingStore {
	if now == nil {
		now = time.Now
	}
	return &redisBindingStore{rs: rs, now: now, backstopTTL: 10 * time.Minute}
}

const (
	bindKeyPrefix = "branch:reg:bind:"
	aorKeyPrefix  = "branch:reg:aor:"
	aorsKey       = "branch:reg:aors"
)

func bindKey(sip, callID string) string { return bindKeyPrefix + sip + ":" + callID }
func aorKey(sip string) string          { return aorKeyPrefix + sip }

func (s *redisBindingStore) Upsert(b branch.RegistrationBinding) error {
	raw, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("marshal binding: %w", err)
	}
	if err := s.rs.SetString(bindKey(b.SIPUser, b.CallID), string(raw), s.backstopTTL); err != nil {
		return err
	}
	if err := s.rs.SAdd(aorKey(b.SIPUser), b.CallID); err != nil {
		return err
	}
	// Keep the contact index alive at least as long as any contact could be.
	_, _ = s.rs.Expire(aorKey(b.SIPUser), s.backstopTTL)
	return s.rs.SAdd(aorsKey, b.SIPUser)
}

func (s *redisBindingStore) Remove(sipUser, callID string) error {
	if err := s.rs.Del(bindKey(sipUser, callID)); err != nil {
		return err
	}
	if err := s.rs.SRem(aorKey(sipUser), callID); err != nil {
		return err
	}
	return s.pruneAORIfEmpty(sipUser)
}

func (s *redisBindingStore) ListLive(sipUser string) ([]branch.RegistrationBinding, error) {
	callIDs, err := s.rs.SMembers(aorKey(sipUser))
	if err != nil || len(callIDs) == 0 {
		return nil, err
	}
	now := s.now()
	out := make([]branch.RegistrationBinding, 0, len(callIDs))
	for _, callID := range callIDs {
		raw, err := s.rs.GetString(bindKey(sipUser, callID))
		if err != nil {
			return nil, err
		}
		if raw == "" {
			// The Redis backstop already dropped this contact: prune the stale index entry.
			_ = s.rs.SRem(aorKey(sipUser), callID)
			continue
		}
		var b branch.RegistrationBinding
		if err := json.Unmarshal([]byte(raw), &b); err != nil {
			continue
		}
		if b.Expired(now) {
			// Logically expired but Redis has not dropped it yet: drop it now.
			_ = s.rs.Del(bindKey(sipUser, callID))
			_ = s.rs.SRem(aorKey(sipUser), callID)
			continue
		}
		out = append(out, b)
	}
	_ = s.pruneAORIfEmpty(sipUser)
	return out, nil
}

func (s *redisBindingStore) CountLive(sipUser string) (int, error) {
	live, err := s.ListLive(sipUser)
	return len(live), err
}

// ReapExpired evicts every logically-expired contact and RETURNS the evicted bindings,
// preserving the in-memory store's contract so the registrar's reaper can drive the
// presence-offline transition (OnBranchUnreachable + dashboard status) exactly as before.
func (s *redisBindingStore) ReapExpired(now time.Time) []branch.RegistrationBinding {
	sips, err := s.rs.SMembers(aorsKey)
	if err != nil {
		return nil
	}
	var evicted []branch.RegistrationBinding
	for _, sip := range sips {
		callIDs, err := s.rs.SMembers(aorKey(sip))
		if err != nil {
			continue
		}
		for _, callID := range callIDs {
			raw, err := s.rs.GetString(bindKey(sip, callID))
			if err != nil {
				continue
			}
			if raw == "" {
				// Redis backstop already dropped it; index-only cleanup (binding data lost).
				_ = s.rs.SRem(aorKey(sip), callID)
				continue
			}
			var b branch.RegistrationBinding
			if err := json.Unmarshal([]byte(raw), &b); err != nil {
				continue
			}
			if b.Expired(now) {
				evicted = append(evicted, b)
				_ = s.rs.Del(bindKey(sip, callID))
				_ = s.rs.SRem(aorKey(sip), callID)
			}
		}
		_ = s.pruneAORIfEmpty(sip)
	}
	return evicted
}

// ListAllLive returns every live binding across all AORs, the boot-rehydration source
// the registrar uses to rebuild routing sessions after a restart.
func (s *redisBindingStore) ListAllLive() ([]branch.RegistrationBinding, error) {
	sips, err := s.rs.SMembers(aorsKey)
	if err != nil {
		return nil, err
	}
	var all []branch.RegistrationBinding
	for _, sip := range sips {
		live, err := s.ListLive(sip)
		if err != nil {
			return nil, err
		}
		all = append(all, live...)
	}
	return all, nil
}

// pruneAORIfEmpty drops the sip from the global AOR index once it has no live contacts,
// so branch:reg:aors does not accumulate dead entries.
func (s *redisBindingStore) pruneAORIfEmpty(sipUser string) error {
	members, err := s.rs.SMembers(aorKey(sipUser))
	if err != nil {
		return err
	}
	if len(members) == 0 {
		if err := s.rs.Del(aorKey(sipUser)); err != nil {
			return err
		}
		return s.rs.SRem(aorsKey, sipUser)
	}
	return nil
}

// BindingRehydrator is the optional capability the registrar type-asserts on boot to
// rebuild routing from a durable store. The in-memory store does not implement it (there
// is nothing to rehydrate: it starts empty).
type BindingRehydrator interface {
	ListAllLive() ([]branch.RegistrationBinding, error)
}

var _ BindingRehydrator = (*redisBindingStore)(nil)
