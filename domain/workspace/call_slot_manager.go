package workspace

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"vozko/domain/cache"
	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

// TODO: validate this TTL, as it should be PER CALL, not per workspace, so when an workspace tried dialing,
// it aways refreshs even if a call hanged or something like that, (IGNORE FOR NOW AS WE DONT HAVE THE LEAK SOURCE)
const SlotKeyTTL = 45 * time.Second

const SlotHeartbeatInterval = 15 * time.Second

const SlotCapacityLogInterval = 30 * time.Second

type CallSlotManager struct {
	shared       cache.SharedState
	subRepo      workspace_plan.CurrentSubscriptionReader
	planReader   workspace_plan.PlanReader
	entitlements workspace_addon.EntitlementResolver
	replicaID    string

	activeWorkspacesMu sync.Mutex
	activeWorkspaces   map[string]int

	capacityLogMu   sync.Mutex
	lastCapacityLog map[string]time.Time
}

func NewCallSlotManager(
	shared cache.SharedState,
	subRepo workspace_plan.CurrentSubscriptionReader,
	planReader workspace_plan.PlanReader,
	replicaID string,
) *CallSlotManager {
	return &CallSlotManager{
		shared:           shared,
		subRepo:          subRepo,
		planReader:       planReader,
		replicaID:        replicaID,
		activeWorkspaces: make(map[string]int),
		lastCapacityLog:  make(map[string]time.Time),
	}
}

func (m *CallSlotManager) WithEntitlements(resolver workspace_addon.EntitlementResolver) *CallSlotManager {
	if m != nil {
		m.entitlements = resolver
	}
	return m
}

type AcquireFailReason int

const (
	AcquireFailNone AcquireFailReason = iota

	AcquireFailAtCapacity

	AcquireFailNoSubscription

	AcquireFailConfig
)

type AcquireResult struct {
	GlobalMax    int64
	WorkspaceMax int64
	WorkspaceID  string
	FailReason   AcquireFailReason
}

func (m *CallSlotManager) Acquire(workspaceID string, globalMax int64) (AcquireResult, bool) {
	res := AcquireResult{GlobalMax: globalMax, WorkspaceID: workspaceID}
	if m == nil || m.shared == nil || m.subRepo == nil || m.planReader == nil || workspaceID == "" || globalMax <= 0 {
		log.Printf("call slot manager: invalid configuration or input for workspace %s", workspaceID)
		res.FailReason = AcquireFailConfig
		return res, false
	}

	ok, err := m.shared.TryIncr("calls:active:count", globalMax)
	if err != nil {
		log.Printf("call slot manager: global TryIncr error: %v", err)
		res.FailReason = AcquireFailAtCapacity
		return res, false
	}
	if !ok {
		res.FailReason = AcquireFailAtCapacity
		return res, false
	}

	_, _ = m.shared.Incr("calls:active:count:" + m.replicaID)

	workspaceMax, err := m.resolveWorkspaceLimit(workspaceID)
	if err != nil {
		log.Printf("call slot manager: failed to resolve current subscription for workspace %s: %v", workspaceID, err)
		m.decrGlobal()
		res.FailReason = AcquireFailNoSubscription
		return res, false
	}
	res.WorkspaceMax = workspaceMax

	workspaceKey := workspaceCallKey(workspaceID)
	ok, err = m.shared.TryIncr(workspaceKey, workspaceMax)
	if err != nil {
		log.Printf("call slot manager: workspace TryIncr error for %s: %v", workspaceID, err)
		m.decrGlobal()
		res.FailReason = AcquireFailAtCapacity
		return res, false
	}
	if !ok {
		m.logWorkspaceAtCapacity(workspaceID, workspaceMax)
		m.decrGlobal()
		res.FailReason = AcquireFailAtCapacity
		return res, false
	}

	m.shared.HIncrBy(workspaceSlotsKey(m.replicaID), workspaceID, 1)

	m.shared.Expire("calls:active:count", SlotKeyTTL)
	m.shared.Expire("calls:active:count:"+m.replicaID, SlotKeyTTL)
	m.shared.Expire(workspaceKey, SlotKeyTTL)
	m.shared.Expire(workspaceSlotsKey(m.replicaID), SlotKeyTTL)

	m.activeWorkspacesMu.Lock()
	m.activeWorkspaces[workspaceID]++
	m.activeWorkspacesMu.Unlock()

	return res, true
}

func (m *CallSlotManager) Release(workspaceID string) {
	if m == nil || m.shared == nil || workspaceID == "" {
		return
	}
	m.decrGlobal()
	newCount, err := m.shared.Decr(workspaceCallKey(workspaceID))
	if err != nil {
		log.Printf("call slot manager: workspace Decr error for %s: %v", workspaceID, err)
		return
	}

	newWorkspaceSlot, _ := m.shared.HIncrBy(workspaceSlotsKey(m.replicaID), workspaceID, -1)
	if newWorkspaceSlot <= 0 {
		_ = m.shared.HDel(workspaceSlotsKey(m.replicaID), workspaceID)
	}

	m.activeWorkspacesMu.Lock()
	m.activeWorkspaces[workspaceID]--
	clearCapacityLog := false
	if m.activeWorkspaces[workspaceID] <= 0 {
		delete(m.activeWorkspaces, workspaceID)
		clearCapacityLog = true
	}
	m.activeWorkspacesMu.Unlock()
	if clearCapacityLog {
		m.clearWorkspaceCapacityLog(workspaceID)
	}

	log.Printf("call slot manager: released workspace slot for %s (active: %d)", workspaceID, newCount)
}

func (m *CallSlotManager) logWorkspaceAtCapacity(workspaceID string, workspaceMax int64) {
	if m == nil || workspaceID == "" {
		return
	}

	now := time.Now()
	m.capacityLogMu.Lock()
	lastLoggedAt, ok := m.lastCapacityLog[workspaceID]
	if ok && now.Sub(lastLoggedAt) < SlotCapacityLogInterval {
		m.capacityLogMu.Unlock()
		return
	}
	m.lastCapacityLog[workspaceID] = now
	m.capacityLogMu.Unlock()

	log.Printf("call slot manager: workspace %s at capacity (%d)", workspaceID, workspaceMax)
}

func (m *CallSlotManager) clearWorkspaceCapacityLog(workspaceID string) {
	if m == nil || workspaceID == "" {
		return
	}

	m.capacityLogMu.Lock()
	delete(m.lastCapacityLog, workspaceID)
	m.capacityLogMu.Unlock()
}

func (m *CallSlotManager) StartupCleanup() {
	replicas, err := m.shared.SMembers("calls:replicas")
	if err != nil {
		log.Printf("call slot manager: startup cleanup — failed to list replicas: %v", err)
		return
	}
	for _, rid := range replicas {
		alive, _ := m.shared.Exists("replica:heartbeat:" + rid)
		if alive {
			continue
		}
		m.recoverStaleReplica(rid)
	}
}

func (m *CallSlotManager) recoverStaleReplica(rid string) {

	countStr, _ := m.shared.GetString("calls:active:count:" + rid)
	var staleGlobal int
	if countStr != "" {
		fmt.Sscanf(countStr, "%d", &staleGlobal)
	}
	if staleGlobal > 0 {
		for i := 0; i < staleGlobal; i++ {
			_, _ = m.shared.Decr("calls:active:count")
		}
	}

	workspaceCounts, _ := m.shared.HGetAll(workspaceSlotsKey(rid))
	for workspaceID, cStr := range workspaceCounts {
		var count int
		fmt.Sscanf(cStr, "%d", &count)
		if count > 0 {
			for i := 0; i < count; i++ {
				_, _ = m.shared.Decr(workspaceCallKey(workspaceID))
			}
			log.Printf("call slot manager: recovered %d leaked workspace slots for %s from stale replica %s", count, workspaceID, rid)
		}
	}

	_ = m.shared.Del("calls:active:count:" + rid)
	_ = m.shared.Del(workspaceSlotsKey(rid))
	_ = m.shared.SRem("calls:replicas", rid)
	if staleGlobal > 0 || len(workspaceCounts) > 0 {
		log.Printf("call slot manager: recovered %d leaked global slots from stale replica %s", staleGlobal, rid)
	}
}

func (m *CallSlotManager) RunHeartbeat(ctx context.Context) {

	m.refreshTTLs()

	ticker := time.NewTicker(SlotHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.refreshTTLs()
		}
	}
}

func (m *CallSlotManager) RunPeriodicCleanup(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			replicas, err := m.shared.SMembers("calls:replicas")
			if err != nil {
				continue
			}
			for _, rid := range replicas {
				if rid == m.replicaID {
					continue
				}
				alive, _ := m.shared.Exists("replica:heartbeat:" + rid)
				if alive {
					continue
				}
				m.recoverStaleReplica(rid)
			}
		}
	}
}

func (m *CallSlotManager) refreshTTLs() {

	m.shared.Expire("calls:active:count", SlotKeyTTL)

	m.shared.Expire("calls:active:count:"+m.replicaID, SlotKeyTTL)
	m.shared.Expire(workspaceSlotsKey(m.replicaID), SlotKeyTTL)

	m.activeWorkspacesMu.Lock()
	workspaces := make([]string, 0, len(m.activeWorkspaces))
	for workspaceID := range m.activeWorkspaces {
		workspaces = append(workspaces, workspaceID)
	}
	m.activeWorkspacesMu.Unlock()

	for _, workspaceID := range workspaces {
		m.shared.Expire(workspaceCallKey(workspaceID), SlotKeyTTL)
	}
}

// WorkspaceLimit returns the concurrent call channel max for the workspace
// (entitlements / plan). Safe for read-only board capacity.
func (m *CallSlotManager) WorkspaceLimit(workspaceID string) (int64, error) {
	return m.resolveWorkspaceLimit(workspaceID)
}

func (m *CallSlotManager) resolveWorkspaceLimit(workspaceID string) (int64, error) {
	if m.entitlements != nil {
		limit, err := m.entitlements.Resolve(workspaceID, workspace_addon.EntitlementCallChannels)
		if err != nil {
			return 0, err
		}
		if limit <= 0 {
			return 0, fmt.Errorf("workspace %s has no call channel entitlement", workspaceID)
		}
		return int64(limit), nil
	}
	subscription, err := m.subRepo.GetCurrentByWorkspaceID(workspaceID, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	if subscription == nil {
		return 0, fmt.Errorf("workspace %s has no current subscription", workspaceID)
	}
	if subscription.Status != workspace_plan.SubscriptionStatusActive {
		return 0, fmt.Errorf("workspace %s subscription is not active: %w", workspaceID, workspace_plan.ErrSubscriptionNotActive)
	}
	plan, err := m.planReader.GetByID(subscription.PlanDefinitionID)
	if err != nil {
		return 0, fmt.Errorf("workspace %s: failed to read plan %s: %w", workspaceID, subscription.PlanDefinitionID, err)
	}
	if plan == nil || plan.MaxCallChannels <= 0 {
		return 0, fmt.Errorf("workspace %s has no valid max call channels in plan %s", workspaceID, subscription.PlanDefinitionID)
	}
	return int64(plan.MaxCallChannels), nil
}

func (m *CallSlotManager) decrGlobal() {
	_, _ = m.shared.Decr("calls:active:count")
	_, _ = m.shared.Decr("calls:active:count:" + m.replicaID)
}

func workspaceCallKey(workspaceID string) string {
	return "calls:active:count:ws:" + workspaceID
}

func workspaceSlotsKey(replicaID string) string {
	return "calls:workspace_slots:" + replicaID
}
