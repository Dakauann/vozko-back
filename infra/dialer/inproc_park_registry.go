package dialer

import (
	"errors"
	"sync"

	dialer_domain "vozko/domain/dialer"
)

// InProcParkRegistry holds parked caller legs (see dialer_domain.ParkRegistry).
// One entry per call, owner-token CAS on Retrieve/Abandon (the same discipline as
// the ring ReservationState), and a per-entry death watcher on the leg's Done
// channel that fires the customer-hangup funnel exactly once — and only while the
// leg is still parked. In-process, matching the transfer store and call registry.
type InProcParkRegistry struct {
	mu   sync.Mutex
	byWS map[string]map[string]*parkedEntry
}

type parkedEntry struct {
	leg     dialer_domain.CallLeg
	owner   string
	disarm  chan struct{}
	watched bool
}

func NewInProcParkRegistry() *InProcParkRegistry {
	return &InProcParkRegistry{byWS: make(map[string]map[string]*parkedEntry)}
}

var errParkOccupied = errors.New("park registry: a leg is already parked for this call")

func (r *InProcParkRegistry) Park(workspaceID string, leg dialer_domain.CallLeg, ownerToken string, onDeath func()) error {
	if leg == nil || workspaceID == "" || ownerToken == "" {
		return errors.New("park registry: workspace, leg and owner token are required")
	}
	callID := leg.CallID()
	if callID == "" {
		return errors.New("park registry: leg has no call id")
	}

	r.mu.Lock()
	calls, ok := r.byWS[workspaceID]
	if !ok {
		calls = make(map[string]*parkedEntry)
		r.byWS[workspaceID] = calls
	}
	if existing, ok := calls[callID]; ok && existing.owner != ownerToken {
		r.mu.Unlock()
		return errParkOccupied
	}
	entry := &parkedEntry{leg: leg, owner: ownerToken, disarm: make(chan struct{})}
	calls[callID] = entry
	r.mu.Unlock()

	// Death watcher: if the caller hangs up while parked, remove the entry FIRST
	// (so a racing Retrieve/Abandon sees it gone) and only then fire the funnel.
	// Retrieve/Abandon disarm it, so a leg handed to a new owner never fires.
	done := leg.Done()
	if done == nil {
		return nil
	}
	go func() {
		select {
		case <-entry.disarm:
			return
		case <-done:
			if r.removeIfCurrent(workspaceID, callID, entry) && onDeath != nil {
				onDeath()
			}
		}
	}()
	return nil
}

// removeIfCurrent removes the entry iff it is still the one registered for the
// call, returning whether this caller performed the removal (the "exactly once"
// gate between the death watcher and Retrieve/Abandon).
func (r *InProcParkRegistry) removeIfCurrent(workspaceID, callID string, entry *parkedEntry) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	calls, ok := r.byWS[workspaceID]
	if !ok {
		return false
	}
	current, ok := calls[callID]
	if !ok || current != entry {
		return false
	}
	delete(calls, callID)
	if len(calls) == 0 {
		delete(r.byWS, workspaceID)
	}
	return true
}

func (r *InProcParkRegistry) take(workspaceID, callID, ownerToken string) (dialer_domain.CallLeg, bool) {
	r.mu.Lock()
	calls, ok := r.byWS[workspaceID]
	if !ok {
		r.mu.Unlock()
		return nil, false
	}
	entry, ok := calls[callID]
	if !ok || entry.owner != ownerToken {
		r.mu.Unlock()
		return nil, false
	}
	delete(calls, callID)
	if len(calls) == 0 {
		delete(r.byWS, workspaceID)
	}
	r.mu.Unlock()

	close(entry.disarm)
	return entry.leg, true
}

func (r *InProcParkRegistry) Retrieve(workspaceID, callID, ownerToken string) (dialer_domain.CallLeg, bool) {
	return r.take(workspaceID, callID, ownerToken)
}

func (r *InProcParkRegistry) Abandon(workspaceID, callID, ownerToken string) (dialer_domain.CallLeg, bool) {
	return r.take(workspaceID, callID, ownerToken)
}

func (r *InProcParkRegistry) Find(workspaceID, callID string) (dialer_domain.ParkedLeg, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	calls, ok := r.byWS[workspaceID]
	if !ok {
		return dialer_domain.ParkedLeg{}, false
	}
	entry, ok := calls[callID]
	if !ok {
		return dialer_domain.ParkedLeg{}, false
	}
	return dialer_domain.ParkedLeg{
		CallID:      callID,
		WorkspaceID: workspaceID,
		OwnerToken:  entry.owner,
		Leg:         entry.leg,
	}, true
}

var _ dialer_domain.ParkRegistry = (*InProcParkRegistry)(nil)
