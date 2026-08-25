package callsession

import (
	"sync"

	callsession_domain "vozko/domain/callsession"
)

type InProcCallRegistry struct {
	mu   sync.RWMutex
	byWS map[string]map[string]*callsession_domain.CallEntry
}

func NewInProcCallRegistry() *InProcCallRegistry {
	return &InProcCallRegistry{
		byWS: make(map[string]map[string]*callsession_domain.CallEntry),
	}
}

func (r *InProcCallRegistry) Register(entry callsession_domain.CallEntry) error {
	if entry.WorkspaceID == "" {
		return callsession_domain.ErrWorkspaceRequired
	}
	if entry.CallID == "" {
		return callsession_domain.ErrCallIDRequired
	}
	if entry.OwnerSessionID == "" || entry.OwnerUserID == "" {
		return callsession_domain.ErrOwnerRequired
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	calls, ok := r.byWS[entry.WorkspaceID]
	if !ok {
		calls = make(map[string]*callsession_domain.CallEntry)
		r.byWS[entry.WorkspaceID] = calls
	}
	cp := entry
	calls[entry.CallID] = &cp
	return nil
}

func (r *InProcCallRegistry) Lookup(workspaceID, callID string) (callsession_domain.CallEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	calls, ok := r.byWS[workspaceID]
	if !ok {
		return callsession_domain.CallEntry{}, false
	}
	entry, ok := calls[callID]
	if !ok {
		return callsession_domain.CallEntry{}, false
	}
	return *entry, true
}

func (r *InProcCallRegistry) Unregister(workspaceID, callID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	calls, ok := r.byWS[workspaceID]
	if !ok {
		return
	}
	delete(calls, callID)
	if len(calls) == 0 {
		delete(r.byWS, workspaceID)
	}
}

var _ callsession_domain.CallRegistry = (*InProcCallRegistry)(nil)
