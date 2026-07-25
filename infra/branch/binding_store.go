// Package branchinfra holds single-VPS infrastructure for the branch registrar.
// The in-memory AOR binding store is the "location service" (Asterisk
// ps_contacts equivalent): live registrations kept in process, refreshed by
// re-REGISTER and evicted on expiry. On a single VPS this is sufficient; the
// domain BindingStore port lets a durable Redis/Postgres implementation replace
// it later without touching the use cases.
package branchinfra

import (
	"sync"
	"time"

	"vozko/domain/branch"
)

type inMemoryBindingStore struct {
	mu sync.RWMutex
	// sipUser -> callID -> binding
	byUser map[string]map[string]branch.RegistrationBinding
}

// NewInMemoryBindingStore builds an in-process AOR store.
func NewInMemoryBindingStore() branch.BindingStore {
	return &inMemoryBindingStore{byUser: make(map[string]map[string]branch.RegistrationBinding)}
}

func (s *inMemoryBindingStore) Upsert(b branch.RegistrationBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	contacts := s.byUser[b.SIPUser]
	if contacts == nil {
		contacts = make(map[string]branch.RegistrationBinding)
		s.byUser[b.SIPUser] = contacts
	}
	contacts[b.CallID] = b
	return nil
}

func (s *inMemoryBindingStore) Remove(sipUser, callID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if contacts := s.byUser[sipUser]; contacts != nil {
		delete(contacts, callID)
		if len(contacts) == 0 {
			delete(s.byUser, sipUser)
		}
	}
	return nil
}

func (s *inMemoryBindingStore) ListLive(sipUser string) ([]branch.RegistrationBinding, error) {
	now := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	contacts := s.byUser[sipUser]
	if len(contacts) == 0 {
		return nil, nil
	}
	out := make([]branch.RegistrationBinding, 0, len(contacts))
	for _, b := range contacts {
		if !b.Expired(now) {
			out = append(out, b)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func (s *inMemoryBindingStore) CountLive(sipUser string) (int, error) {
	live, err := s.ListLive(sipUser)
	return len(live), err
}

func (s *inMemoryBindingStore) ReapExpired(now time.Time) []branch.RegistrationBinding {
	s.mu.Lock()
	defer s.mu.Unlock()
	var evicted []branch.RegistrationBinding
	for user, contacts := range s.byUser {
		for callID, b := range contacts {
			if b.Expired(now) {
				evicted = append(evicted, b)
				delete(contacts, callID)
			}
		}
		if len(contacts) == 0 {
			delete(s.byUser, user)
		}
	}
	return evicted
}
