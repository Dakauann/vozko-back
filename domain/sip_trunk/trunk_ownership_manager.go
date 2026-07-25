package sip_trunk

import (
	"context"
	"strings"
	"sync"
	"time"

	"vozko/domain/cache"
)

const (
	TrunkLockTTL = 45 * time.Second

	TrunkLockHeartbeat = 15 * time.Second

	TrunkTakeoverScanInterval = 20 * time.Second
)

func trunkOwnerKey(trunkID string) string {
	return "sip:trunk_owner:" + trunkID
}

func replicaTrunksKey(replicaID string) string {
	return "sip:replica_trunks:" + replicaID
}

type OwnershipCallbacks struct {
	OnAcquired func(trunkID string)
	OnLost     func(trunkID string)
}

type OwnershipLookup interface {
	IsOwner(trunkID string) bool
	FindOwnerAddress(trunkID string) (replicaID, address string, err error)
}

type TrunkOwnershipManager struct {
	shared    cache.SharedState
	replicaID string

	mu        sync.RWMutex
	owned     map[string]struct{}
	callbacks OwnershipCallbacks
}

func NewTrunkOwnershipManager(shared cache.SharedState, replicaID string) *TrunkOwnershipManager {
	return &TrunkOwnershipManager{
		shared:    shared,
		replicaID: replicaID,
		owned:     map[string]struct{}{},
	}
}

func (m *TrunkOwnershipManager) SetCallbacks(cb OwnershipCallbacks) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbacks = cb
}

func (m *TrunkOwnershipManager) ReplicaID() string {
	return m.replicaID
}

func (m *TrunkOwnershipManager) IsOwner(trunkID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.owned[trunkID]
	return ok
}

func (m *TrunkOwnershipManager) OwnedSnapshot() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.owned))
	for id := range m.owned {
		ids = append(ids, id)
	}
	return ids
}

func (m *TrunkOwnershipManager) FindOwner(trunkID string) (string, error) {
	v, err := m.shared.GetString(trunkOwnerKey(trunkID))
	if err != nil {
		return "", err
	}
	return v, nil
}

func (m *TrunkOwnershipManager) FindOwnerAddress(trunkID string) (string, string, error) {
	rid, err := m.FindOwner(trunkID)
	if err != nil || rid == "" {
		return "", "", err
	}
	addr, err := m.shared.GetString("replica:heartbeat:" + rid)
	if err != nil {
		return rid, "", err
	}
	if addr == "1" {
		addr = ""
	}
	return rid, addr, nil
}

func (m *TrunkOwnershipManager) TryAcquire(trunkID string) (bool, error) {
	key := trunkOwnerKey(trunkID)

	ok, err := m.shared.SetNX(key, m.replicaID, TrunkLockTTL)
	if err != nil {
		return false, err
	}
	if ok {
		return m.markOwnedAndNotify(trunkID), nil
	}

	cur, err := m.shared.GetString(key)
	if err != nil {
		return false, err
	}
	if cur != m.replicaID {
		return false, nil
	}

	if _, err := m.shared.Expire(key, TrunkLockTTL); err != nil {
		return false, err
	}
	m.mu.Lock()
	if _, already := m.owned[trunkID]; !already {
		m.owned[trunkID] = struct{}{}
		_ = m.shared.SAdd(replicaTrunksKey(m.replicaID), trunkID)

		cb := m.callbacks.OnAcquired
		m.mu.Unlock()
		if cb != nil {
			cb(trunkID)
		}
		return true, nil
	}
	m.mu.Unlock()
	return true, nil
}

func (m *TrunkOwnershipManager) markOwnedAndNotify(trunkID string) bool {
	m.mu.Lock()
	_, already := m.owned[trunkID]
	if !already {
		m.owned[trunkID] = struct{}{}
	}
	cb := m.callbacks.OnAcquired
	m.mu.Unlock()

	_ = m.shared.SAdd(replicaTrunksKey(m.replicaID), trunkID)

	if !already && cb != nil {
		cb(trunkID)
	}
	return true
}

func (m *TrunkOwnershipManager) Release(trunkID string) error {
	m.mu.Lock()
	_, owned := m.owned[trunkID]
	if !owned {
		m.mu.Unlock()
		return nil
	}
	delete(m.owned, trunkID)
	cb := m.callbacks.OnLost
	m.mu.Unlock()

	cur, err := m.shared.GetString(trunkOwnerKey(trunkID))
	if err == nil && cur == m.replicaID {
		_ = m.shared.Del(trunkOwnerKey(trunkID))
	}
	_ = m.shared.SRem(replicaTrunksKey(m.replicaID), trunkID)

	if cb != nil {
		cb(trunkID)
	}
	return nil
}

func (m *TrunkOwnershipManager) RunHeartbeat(ctx context.Context) {
	t := time.NewTicker(TrunkLockHeartbeat)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.refreshAllLocks()
		}
	}
}

func (m *TrunkOwnershipManager) refreshAllLocks() {
	m.mu.RLock()
	ids := make([]string, 0, len(m.owned))
	for id := range m.owned {
		ids = append(ids, id)
	}
	cb := m.callbacks.OnLost
	m.mu.RUnlock()

	for _, id := range ids {
		key := trunkOwnerKey(id)
		cur, err := m.shared.GetString(key)
		if err != nil {

			continue
		}
		if cur != m.replicaID {

			m.mu.Lock()
			delete(m.owned, id)
			m.mu.Unlock()
			_ = m.shared.SRem(replicaTrunksKey(m.replicaID), id)
			if cb != nil {
				cb(id)
			}
			continue
		}
		_, _ = m.shared.Expire(key, TrunkLockTTL)
	}
}

func (m *TrunkOwnershipManager) RunTakeover(
	ctx context.Context,
	listEnabled func() ([]*SIPTrunk, error),
) {
	t := time.NewTicker(TrunkTakeoverScanInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.takeoverPass(listEnabled)
		}
	}
}

func (m *TrunkOwnershipManager) takeoverPass(listEnabled func() ([]*SIPTrunk, error)) {
	trunks, err := listEnabled()
	if err != nil {
		return
	}
	for _, tr := range trunks {
		if tr == nil {
			continue
		}
		if m.IsOwner(tr.ID) {
			continue
		}
		_, _ = m.TryAcquire(tr.ID)
	}
}

type LiveReplicasSource interface {
	LiveReplicas() ([]string, error)
}

func HashKey(t *SIPTrunk) string {
	if t == nil {
		return ""
	}
	ws := strings.TrimSpace(t.WorkspaceID)
	if ws == "" {
		return "trunk:" + t.ID
	}
	return "ws:" + ws
}

func (m *TrunkOwnershipManager) RunReconciler(
	ctx context.Context,
	replicas LiveReplicasSource,
	listEnabled func() ([]*SIPTrunk, error),
) {
	t := time.NewTicker(TrunkTakeoverScanInterval)
	defer t.Stop()

	m.reconcilePass(replicas, listEnabled)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.reconcilePass(replicas, listEnabled)
		}
	}
}

func (m *TrunkOwnershipManager) ReconcileOnce(
	replicas LiveReplicasSource,
	listEnabled func() ([]*SIPTrunk, error),
) {
	m.reconcilePass(replicas, listEnabled)
}

func (m *TrunkOwnershipManager) reconcilePass(
	replicas LiveReplicasSource,
	listEnabled func() ([]*SIPTrunk, error),
) {
	live, err := replicas.LiveReplicas()
	if err != nil || len(live) == 0 {
		return
	}
	trunks, err := listEnabled()
	if err != nil {
		return
	}

	keyOwner := map[string]string{}
	for _, tr := range trunks {
		if tr == nil {
			continue
		}
		k := HashKey(tr)
		if _, seen := keyOwner[k]; !seen {
			keyOwner[k] = AssignOwner(k, live)
		}
	}

	liveSet := make(map[string]struct{}, len(live))
	for _, rid := range live {
		liveSet[rid] = struct{}{}
	}

	for _, tr := range trunks {
		if tr == nil {
			continue
		}
		owner := keyOwner[HashKey(tr)]
		if owner != m.replicaID {
			continue
		}
		if m.IsOwner(tr.ID) {
			continue
		}
		if cur, err := m.shared.GetString(trunkOwnerKey(tr.ID)); err == nil && cur != "" && cur != m.replicaID {
			if _, alive := liveSet[cur]; !alive {
				_ = m.shared.Del(trunkOwnerKey(tr.ID))
			}
		}
		_, _ = m.TryAcquire(tr.ID)
	}

	for _, tr := range trunks {
		if tr == nil {
			continue
		}
		if !m.IsOwner(tr.ID) {
			continue
		}
		if keyOwner[HashKey(tr)] == m.replicaID {
			continue
		}
		_ = m.Release(tr.ID)
	}
}
