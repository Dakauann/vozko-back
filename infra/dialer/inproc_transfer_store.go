package dialer

import (
	"sync"
	"time"

	dialer_domain "vozko/domain/dialer"
)

type InProcTransferStore struct {
	mu     sync.RWMutex
	byID   map[string]*dialer_domain.TransferHandle
	byCall map[callKey]string
}

type callKey struct {
	workspaceID string
	callID      string
}

func NewInProcTransferStore() *InProcTransferStore {
	return &InProcTransferStore{
		byID:   make(map[string]*dialer_domain.TransferHandle),
		byCall: make(map[callKey]string),
	}
}

func (s *InProcTransferStore) Put(h *dialer_domain.TransferHandle) error {
	if h == nil || h.ID == "" {
		return dialer_domain.ErrTransferNotFound
	}
	if h.CallID == "" || h.WorkspaceID == "" {
		return dialer_domain.ErrTransferCallNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := callKey{h.WorkspaceID, h.CallID}
	if existingID, ok := s.byCall[key]; ok && existingID != h.ID {
		if existing, ok := s.byID[existingID]; ok && !existing.Stage.Terminal() {
			return dialer_domain.ErrTransferAlreadyInFlight
		}
	}

	cp := *h
	s.byID[h.ID] = &cp
	if !h.Stage.Terminal() {
		s.byCall[key] = h.ID
	}
	return nil
}

func (s *InProcTransferStore) Get(transferID string) (*dialer_domain.TransferHandle, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.byID[transferID]
	if !ok {
		return nil, false
	}
	cp := *h
	return &cp, true
}

func (s *InProcTransferStore) Update(transferID string, mut func(*dialer_domain.TransferHandle) error) (*dialer_domain.TransferHandle, error) {
	if mut == nil {
		return nil, dialer_domain.ErrTransferNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.byID[transferID]
	if !ok {
		return nil, dialer_domain.ErrTransferNotFound
	}
	if err := mut(h); err != nil {
		return nil, err
	}
	h.UpdatedAt = time.Now().UTC()

	key := callKey{h.WorkspaceID, h.CallID}
	if h.Stage.Terminal() {
		if existingID, ok := s.byCall[key]; ok && existingID == h.ID {
			delete(s.byCall, key)
		}
	}
	cp := *h
	return &cp, nil
}

func (s *InProcTransferStore) FindByCall(workspaceID, callID string) (*dialer_domain.TransferHandle, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byCall[callKey{workspaceID, callID}]
	if !ok {
		return nil, false
	}
	h, ok := s.byID[id]
	if !ok {
		return nil, false
	}
	cp := *h
	return &cp, true
}

func (s *InProcTransferStore) Delete(transferID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.byID[transferID]
	if !ok {
		return
	}
	key := callKey{h.WorkspaceID, h.CallID}
	if existingID, ok := s.byCall[key]; ok && existingID == transferID {
		delete(s.byCall, key)
	}
	delete(s.byID, transferID)
}

func (s *InProcTransferStore) ListByStage(stage dialer_domain.TransferStage) []*dialer_domain.TransferHandle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*dialer_domain.TransferHandle
	for _, h := range s.byID {
		if h.Stage != stage {
			continue
		}
		cp := *h
		out = append(out, &cp)
	}
	return out
}

func (s *InProcTransferStore) ExpireBefore(cutoff time.Time) []*dialer_domain.TransferHandle {
	s.mu.Lock()
	defer s.mu.Unlock()
	var expired []*dialer_domain.TransferHandle
	for id, h := range s.byID {
		if h.Stage != dialer_domain.TransferStagePendingOffer {
			continue
		}
		if h.UpdatedAt.After(cutoff) {
			continue
		}
		h.Stage = dialer_domain.TransferStageTimedOut
		h.UpdatedAt = time.Now().UTC()
		key := callKey{h.WorkspaceID, h.CallID}
		if existingID, ok := s.byCall[key]; ok && existingID == id {
			delete(s.byCall, key)
		}
		cp := *h
		expired = append(expired, &cp)
	}
	return expired
}

var _ dialer_domain.TransferStore = (*InProcTransferStore)(nil)
