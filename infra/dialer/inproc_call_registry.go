package dialer

import (
	"sync"

	dialer_domain "vozko/domain/dialer"
)

type InProcCallRegistry struct {
	mu   sync.RWMutex
	byWS map[string]map[string]*dialer_domain.DialerCallEntry
}

func NewInProcCallRegistry() *InProcCallRegistry {
	return &InProcCallRegistry{
		byWS: make(map[string]map[string]*dialer_domain.DialerCallEntry),
	}
}

func (r *InProcCallRegistry) Register(entry dialer_domain.DialerCallEntry) error {
	if entry.WorkspaceID == "" {
		return dialer_domain.ErrWorkspaceRequired
	}
	if entry.CallID == "" {
		return dialer_domain.ErrTransferCallNotFound
	}
	if entry.OwnerSessionID == "" || entry.OwnerUserID == "" {
		return dialer_domain.ErrOwnerRequired
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	calls, ok := r.byWS[entry.WorkspaceID]
	if !ok {
		calls = make(map[string]*dialer_domain.DialerCallEntry)
		r.byWS[entry.WorkspaceID] = calls
	}
	cp := entry
	calls[entry.CallID] = &cp
	return nil
}

func (r *InProcCallRegistry) Lookup(workspaceID, callID string) (dialer_domain.DialerCallEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	calls, ok := r.byWS[workspaceID]
	if !ok {
		return dialer_domain.DialerCallEntry{}, false
	}
	entry, ok := calls[callID]
	if !ok {
		return dialer_domain.DialerCallEntry{}, false
	}
	return *entry, true
}

func (r *InProcCallRegistry) Rebind(workspaceID, callID, expectedOwnerSessionID, newOwnerSessionID, newOwnerUserID string) error {
	if newOwnerSessionID == "" || newOwnerUserID == "" {
		return dialer_domain.ErrOwnerRequired
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	calls, ok := r.byWS[workspaceID]
	if !ok {
		return dialer_domain.ErrTransferCallNotFound
	}
	entry, ok := calls[callID]
	if !ok {
		return dialer_domain.ErrTransferCallNotFound
	}
	if entry.OwnerSessionID != expectedOwnerSessionID {
		return dialer_domain.ErrTransferAlreadyInFlight
	}
	entry.OwnerSessionID = newOwnerSessionID
	entry.OwnerUserID = newOwnerUserID
	return nil
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

var _ dialer_domain.DialerCallRegistry = (*InProcCallRegistry)(nil)
