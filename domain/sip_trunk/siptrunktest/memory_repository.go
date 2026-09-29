package siptrunktest

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"vozko/domain/sip_trunk"
)

type MemoryRepository struct {
	mu     sync.Mutex
	trunks map[string]*sip_trunk.SIPTrunk
	nextID int
}

var _ sip_trunk.Repository = (*MemoryRepository)(nil)

func NewMemoryRepository(trunks ...*sip_trunk.SIPTrunk) *MemoryRepository {
	r := &MemoryRepository{trunks: map[string]*sip_trunk.SIPTrunk{}}
	for _, trunk := range trunks {
		copied := *trunk
		r.trunks[trunk.ID] = &copied
	}
	return r
}

func (r *MemoryRepository) Create(_ context.Context, trunk *sip_trunk.SIPTrunk) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	trunk.ID = fmt.Sprintf("trunk-%d", r.nextID)
	trunk.RegistrationStatus = sip_trunk.RegistrationStatusUnregistered
	trunk.CreatedAt = time.Now()
	trunk.UpdatedAt = trunk.CreatedAt
	copied := *trunk
	r.trunks[trunk.ID] = &copied
	return nil
}

func (r *MemoryRepository) Update(_ context.Context, trunk *sip_trunk.SIPTrunk) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.owned(trunk.WorkspaceID, trunk.ID)
	if !ok {
		return sip_trunk.ErrTrunkNotFound
	}
	copied := *trunk
	if copied.Password == "" {
		copied.Password = existing.Password
	}
	copied.RegistrationStatus, copied.LastError = existing.RegistrationStatus, existing.LastError
	copied.UpdatedAt = time.Now()
	r.trunks[trunk.ID] = &copied
	return nil
}

func (r *MemoryRepository) Delete(_ context.Context, workspaceID, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.owned(workspaceID, id); !ok {
		return sip_trunk.ErrTrunkNotFound
	}
	delete(r.trunks, id)
	return nil
}

func (r *MemoryRepository) FindInWorkspace(_ context.Context, workspaceID, id string) (*sip_trunk.SIPTrunk, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.owned(workspaceID, id)
	if !ok {
		return nil, sip_trunk.ErrTrunkNotFound
	}
	copied := *existing
	return &copied, nil
}

func (r *MemoryRepository) ListByWorkspace(_ context.Context, workspaceID string) ([]*sip_trunk.SIPTrunk, error) {
	return r.filter(func(t *sip_trunk.SIPTrunk) bool { return t.WorkspaceID == workspaceID }), nil
}

func (r *MemoryRepository) FindEnabled(context.Context) ([]*sip_trunk.SIPTrunk, error) {
	return r.filter(func(t *sip_trunk.SIPTrunk) bool { return t.Enabled }), nil
}

func (r *MemoryRepository) UpdateStatus(_ context.Context, id string, status sip_trunk.RegistrationStatus, lastError string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if trunk, ok := r.trunks[id]; ok {
		trunk.RegistrationStatus, trunk.LastError = status, lastError
	}
	return nil
}

func (r *MemoryRepository) Stored(id string) (*sip_trunk.SIPTrunk, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	trunk, ok := r.trunks[id]
	if !ok {
		return nil, false
	}
	copied := *trunk
	return &copied, true
}

func (r *MemoryRepository) owned(workspaceID, id string) (*sip_trunk.SIPTrunk, bool) {
	trunk, ok := r.trunks[id]
	if !ok || trunk.WorkspaceID != workspaceID {
		return nil, false
	}
	return trunk, true
}

func (r *MemoryRepository) filter(keep func(*sip_trunk.SIPTrunk) bool) []*sip_trunk.SIPTrunk {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*sip_trunk.SIPTrunk
	for _, trunk := range r.trunks {
		if keep(trunk) {
			copied := *trunk
			out = append(out, &copied)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
