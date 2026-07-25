package workspace

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"vozko/domain/cache"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type mockSharedState struct {
	mu       sync.Mutex
	counters map[string]int64
	strings  map[string]string
	sets     map[string]map[string]bool
	hashes   map[string]map[string]string
	ttls     map[string]time.Duration
	errOnKey map[string]error
}

func newMockSharedState() *mockSharedState {
	return &mockSharedState{
		counters: make(map[string]int64),
		strings:  make(map[string]string),
		sets:     make(map[string]map[string]bool),
		hashes:   make(map[string]map[string]string),
		ttls:     make(map[string]time.Duration),
		errOnKey: make(map[string]error),
	}
}

func (m *mockSharedState) TryIncr(key string, max int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.errOnKey[key]; ok {
		return false, err
	}
	if m.counters[key] >= max {
		return false, nil
	}
	m.counters[key]++
	return true, nil
}

func (m *mockSharedState) Incr(key string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.errOnKey[key]; ok {
		return 0, err
	}
	m.counters[key]++
	return m.counters[key], nil
}

func (m *mockSharedState) Decr(key string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.errOnKey[key]; ok {
		return 0, err
	}
	m.counters[key]--
	return m.counters[key], nil
}

func (m *mockSharedState) get(key string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counters[key]
}

func (m *mockSharedState) SetNX(key, value string, _ time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.strings[key]; ok {
		return false, nil
	}
	m.strings[key] = value
	return true, nil
}

func (m *mockSharedState) SetString(key, value string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.strings[key] = value
	if ttl > 0 {
		m.ttls[key] = ttl
	}
	return nil
}

func (m *mockSharedState) GetString(key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.counters[key]; ok && v != 0 {
		return fmt.Sprintf("%d", v), nil
	}
	if v, ok := m.strings[key]; ok {
		return v, nil
	}
	return "", nil
}

func (m *mockSharedState) Del(keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.counters, k)
		delete(m.strings, k)
		delete(m.sets, k)
		delete(m.hashes, k)
		delete(m.ttls, k)
	}
	return nil
}

func (m *mockSharedState) Exists(key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.strings[key]; ok {
		return true, nil
	}
	if v, ok := m.counters[key]; ok && v != 0 {
		return true, nil
	}
	if s, ok := m.sets[key]; ok && len(s) > 0 {
		return true, nil
	}
	if h, ok := m.hashes[key]; ok && len(h) > 0 {
		return true, nil
	}
	return false, nil
}

func (m *mockSharedState) SAdd(key string, members ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sets[key] == nil {
		m.sets[key] = make(map[string]bool)
	}
	for _, member := range members {
		m.sets[key][member] = true
	}
	return nil
}

func (m *mockSharedState) SRem(key string, members ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sets[key] == nil {
		return nil
	}
	for _, member := range members {
		delete(m.sets[key], member)
	}
	return nil
}

func (m *mockSharedState) SMembers(key string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sets[key]
	if s == nil {
		return nil, nil
	}
	result := make([]string, 0, len(s))
	for member := range s {
		result = append(result, member)
	}
	return result, nil
}

func (m *mockSharedState) Publish(string, []byte) error                    { return nil }
func (m *mockSharedState) Subscribe(context.Context, string, func([]byte)) {}

func (m *mockSharedState) HSet(key, field, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hashes[key] == nil {
		m.hashes[key] = make(map[string]string)
	}
	m.hashes[key][field] = value
	return nil
}

func (m *mockSharedState) HDel(key, field string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hashes[key] != nil {
		delete(m.hashes[key], field)
	}
	return nil
}

func (m *mockSharedState) HGetAll(key string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.hashes[key]
	if h == nil {
		return nil, nil
	}
	clone := make(map[string]string, len(h))
	for field, value := range h {
		clone[field] = value
	}
	return clone, nil
}

func (m *mockSharedState) HIncrBy(key, field string, incr int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hashes[key] == nil {
		m.hashes[key] = make(map[string]string)
	}
	var current int64
	if raw, ok := m.hashes[key][field]; ok {
		fmt.Sscanf(raw, "%d", &current)
	}
	current += incr
	m.hashes[key][field] = fmt.Sprintf("%d", current)
	return current, nil
}

func (m *mockSharedState) Expire(key string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ttls[key] = ttl
	return true, nil
}

func (m *mockSharedState) getTTL(key string) time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ttls[key]
}

func (m *mockSharedState) IncrWithTTL(key string, _ time.Duration) (int64, error) {
	return m.Incr(key)
}

func (m *mockSharedState) IncrBy(key string, amount int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[key] += amount
	return m.counters[key], nil
}

func (m *mockSharedState) DecrBy(key string, amount int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[key] -= amount
	return m.counters[key], nil
}

func (m *mockSharedState) TryIncrBy(key string, delta int64, max int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.counters[key]+delta > max {
		return false, nil
	}
	m.counters[key] += delta
	return true, nil
}

var _ cache.SharedState = (*mockSharedState)(nil)

type mockSubscriptionRepo struct {
	mu              sync.RWMutex
	subscriptions   map[string]*workspace_plan.WorkspaceSubscription
	errsByWorkspace map[string]error
}

type mockPlanReader struct {
	mu    sync.RWMutex
	plans map[string]*workspace_plan.PlanDefinition
}

func newMockSubscriptionRepo() *mockSubscriptionRepo {
	return &mockSubscriptionRepo{
		subscriptions:   make(map[string]*workspace_plan.WorkspaceSubscription),
		errsByWorkspace: make(map[string]error),
	}
}

func newMockPlanReader() *mockPlanReader {
	return &mockPlanReader{
		plans: make(map[string]*workspace_plan.PlanDefinition),
	}
}

func (r *mockPlanReader) GetByID(id string) (*workspace_plan.PlanDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	plan, ok := r.plans[id]
	if !ok {
		return nil, workspace_plan.ErrPlanNotFound
	}
	return plan, nil
}

func setupSubscription(subRepo *mockSubscriptionRepo, planReader *mockPlanReader, workspaceID string, maxCallChannels int) {
	subRepo.mu.Lock()
	defer subRepo.mu.Unlock()
	planReader.mu.Lock()
	defer planReader.mu.Unlock()

	planID := "plan-" + workspaceID
	now := time.Now().UTC()
	subRepo.subscriptions[workspaceID] = &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-" + workspaceID,
		WorkspaceID:        workspaceID,
		PlanDefinitionID:   planID,
		PlanName:           "Plan " + workspaceID,
		MaxCallChannels:    maxCallChannels,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: now.Add(-time.Hour),
		CurrentPeriodEnd:   now.Add(time.Hour),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	delete(subRepo.errsByWorkspace, workspaceID)
	planReader.plans[planID] = &workspace_plan.PlanDefinition{
		ID:              planID,
		Name:            "Plan " + workspaceID,
		MaxCallChannels: maxCallChannels,
	}
}

func (r *mockSubscriptionRepo) SetError(workspaceID string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errsByWorkspace[workspaceID] = err
	delete(r.subscriptions, workspaceID)
}

func (r *mockSubscriptionRepo) GetCurrentByWorkspaceID(workspaceID string, at time.Time) (*workspace_plan.WorkspaceSubscription, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err, ok := r.errsByWorkspace[workspaceID]; ok {
		return nil, err
	}
	subscription, ok := r.subscriptions[workspaceID]
	if !ok {
		return nil, workspace_plan.ErrSubscriptionNotCurrent
	}
	if !subscription.IsCurrent(at) {
		return nil, workspace_plan.ErrSubscriptionNotCurrent
	}
	return subscription, nil
}

func newTestCallSlotManager(replicaID string) (*CallSlotManager, *mockSharedState, *mockSubscriptionRepo, *mockPlanReader) {
	sharedState := newMockSharedState()
	subscriptionRepo := newMockSubscriptionRepo()
	planReader := newMockPlanReader()
	manager := NewCallSlotManager(sharedState, subscriptionRepo, planReader, replicaID)
	return manager, sharedState, subscriptionRepo, planReader
}

func TestAcquire_SingleWorkspace_OK(t *testing.T) {
	mgr, sharedState, subscriptions, planReader := newTestCallSlotManager("replica-1")
	setupSubscription(subscriptions, planReader, "ws-1", 3)

	result, ok := mgr.Acquire("ws-1", 10)
	if !ok {
		t.Fatal("expected acquire to succeed")
	}
	if result.WorkspaceID != "ws-1" {
		t.Fatalf("expected workspace ws-1, got %s", result.WorkspaceID)
	}
	if result.WorkspaceMax != 3 {
		t.Fatalf("expected workspace max 3, got %d", result.WorkspaceMax)
	}
	if result.GlobalMax != 10 {
		t.Fatalf("expected global max 10, got %d", result.GlobalMax)
	}
	if got := sharedState.get("calls:active:count"); got != 1 {
		t.Fatalf("expected global counter 1, got %d", got)
	}
	if got := sharedState.get(workspaceCallKey("ws-1")); got != 1 {
		t.Fatalf("expected workspace counter 1, got %d", got)
	}
	if got := sharedState.get("calls:active:count:replica-1"); got != 1 {
		t.Fatalf("expected replica counter 1, got %d", got)
	}
	hash, _ := sharedState.HGetAll(workspaceSlotsKey("replica-1"))
	if hash["ws-1"] != "1" {
		t.Fatalf("expected replica workspace hash to track ws-1=1, got %v", hash)
	}
	if ttl := sharedState.getTTL(workspaceCallKey("ws-1")); ttl != SlotKeyTTL {
		t.Fatalf("expected workspace TTL %s, got %s", SlotKeyTTL, ttl)
	}
}

func TestAcquire_WorkspaceCapExhausted_RollsBackGlobal(t *testing.T) {
	mgr, sharedState, subscriptions, planReader := newTestCallSlotManager("replica-1")
	setupSubscription(subscriptions, planReader, "ws-1", 2)

	if _, ok := mgr.Acquire("ws-1", 100); !ok {
		t.Fatal("first acquire should succeed")
	}
	if _, ok := mgr.Acquire("ws-1", 100); !ok {
		t.Fatal("second acquire should succeed")
	}
	if _, ok := mgr.Acquire("ws-1", 100); ok {
		t.Fatal("third acquire should fail at workspace cap")
	}
	if got := sharedState.get("calls:active:count"); got != 2 {
		t.Fatalf("expected global counter 2 after rollback, got %d", got)
	}
	if got := sharedState.get(workspaceCallKey("ws-1")); got != 2 {
		t.Fatalf("expected workspace counter 2, got %d", got)
	}
}

func TestAcquire_GlobalCapExhausted(t *testing.T) {
	mgr, sharedState, subscriptions, planReader := newTestCallSlotManager("replica-1")
	setupSubscription(subscriptions, planReader, "ws-1", 10)
	setupSubscription(subscriptions, planReader, "ws-2", 10)

	if _, ok := mgr.Acquire("ws-1", 2); !ok {
		t.Fatal("first acquire should succeed")
	}
	if _, ok := mgr.Acquire("ws-2", 2); !ok {
		t.Fatal("second acquire should succeed")
	}
	if _, ok := mgr.Acquire("ws-1", 2); ok {
		t.Fatal("third acquire should fail at global cap")
	}
	if got := sharedState.get("calls:active:count"); got != 2 {
		t.Fatalf("expected global counter 2, got %d", got)
	}
	if got := sharedState.get(workspaceCallKey("ws-1")); got != 1 {
		t.Fatalf("expected ws-1 counter 1, got %d", got)
	}
	if got := sharedState.get(workspaceCallKey("ws-2")); got != 1 {
		t.Fatalf("expected ws-2 counter 1, got %d", got)
	}
}

func TestAcquire_CancelledSubscription_FailsAsNoSubscription(t *testing.T) {
	mgr, sharedState, subscriptions, planReader := newTestCallSlotManager("replica-1")
	setupSubscription(subscriptions, planReader, "ws-1", 3)

	subscriptions.mu.Lock()
	subscriptions.subscriptions["ws-1"].Status = workspace_plan.SubscriptionStatusCancelled
	subscriptions.mu.Unlock()

	result, ok := mgr.Acquire("ws-1", 10)
	if ok {
		t.Fatal("expected acquire to fail for cancelled subscription")
	}
	if result.FailReason != AcquireFailNoSubscription {
		t.Fatalf("expected fail reason %d, got %d", AcquireFailNoSubscription, result.FailReason)
	}
	if got := sharedState.get("calls:active:count"); got != 0 {
		t.Fatalf("expected global counter rollback to 0, got %d", got)
	}
	if got := sharedState.get(workspaceCallKey("ws-1")); got != 0 {
		t.Fatalf("expected workspace counter 0, got %d", got)
	}
}

func TestRelease_RemovesReplicaWorkspaceTracking(t *testing.T) {
	mgr, sharedState, subscriptions, planReader := newTestCallSlotManager("replica-1")
	setupSubscription(subscriptions, planReader, "ws-1", 3)

	if _, ok := mgr.Acquire("ws-1", 10); !ok {
		t.Fatal("acquire should succeed")
	}
	mgr.Release("ws-1")

	if got := sharedState.get("calls:active:count"); got != 0 {
		t.Fatalf("expected global counter 0, got %d", got)
	}
	if got := sharedState.get(workspaceCallKey("ws-1")); got != 0 {
		t.Fatalf("expected workspace counter 0, got %d", got)
	}
	hash, _ := sharedState.HGetAll(workspaceSlotsKey("replica-1"))
	if _, ok := hash["ws-1"]; ok {
		t.Fatalf("expected ws-1 removed from replica hash, got %v", hash)
	}
	mgr.activeWorkspacesMu.Lock()
	defer mgr.activeWorkspacesMu.Unlock()
	if len(mgr.activeWorkspaces) != 0 {
		t.Fatalf("expected no active workspace tracking, got %v", mgr.activeWorkspaces)
	}
}

func TestAcquire_FailsWithoutCurrentSubscription_RollsBackGlobal(t *testing.T) {
	mgr, sharedState, subscriptions, _ := newTestCallSlotManager("replica-1")
	subscriptions.SetError("ws-1", workspace_plan.ErrSubscriptionNotCurrent)

	if _, ok := mgr.Acquire("ws-1", 10); ok {
		t.Fatal("expected acquire to fail without current subscription")
	}
	if got := sharedState.get("calls:active:count"); got != 0 {
		t.Fatalf("expected global rollback to 0, got %d", got)
	}
	if got := sharedState.get("calls:active:count:replica-1"); got != 0 {
		t.Fatalf("expected replica rollback to 0, got %d", got)
	}
}

func TestStartupCleanup_RecoversStaleReplicaCounters(t *testing.T) {
	mgr, sharedState, _, _ := newTestCallSlotManager("replica-live")
	sharedState.counters["calls:active:count"] = 3
	sharedState.counters["calls:active:count:stale"] = 3
	sharedState.counters[workspaceCallKey("ws-1")] = 2
	sharedState.counters[workspaceCallKey("ws-2")] = 1
	sharedState.hashes[workspaceSlotsKey("stale")] = map[string]string{"ws-1": "2", "ws-2": "1"}
	sharedState.sets["calls:replicas"] = map[string]bool{"stale": true}

	mgr.StartupCleanup()

	if got := sharedState.get("calls:active:count"); got != 0 {
		t.Fatalf("expected global counter 0 after cleanup, got %d", got)
	}
	if got := sharedState.get(workspaceCallKey("ws-1")); got != 0 {
		t.Fatalf("expected ws-1 counter 0 after cleanup, got %d", got)
	}
	if got := sharedState.get(workspaceCallKey("ws-2")); got != 0 {
		t.Fatalf("expected ws-2 counter 0 after cleanup, got %d", got)
	}
	if exists, _ := sharedState.Exists("calls:active:count:stale"); exists {
		t.Fatal("expected stale replica global counter to be removed")
	}
	if exists, _ := sharedState.Exists(workspaceSlotsKey("stale")); exists {
		t.Fatal("expected stale replica workspace hash to be removed")
	}
	replicas, _ := sharedState.SMembers("calls:replicas")
	if len(replicas) != 0 {
		t.Fatalf("expected stale replica removed from replica set, got %v", replicas)
	}
}

func TestRefreshTTLs_RefreshesActiveWorkspaceKeys(t *testing.T) {
	mgr, sharedState, subscriptions, planReader := newTestCallSlotManager("replica-1")
	setupSubscription(subscriptions, planReader, "ws-1", 2)

	if _, ok := mgr.Acquire("ws-1", 10); !ok {
		t.Fatal("acquire should succeed")
	}
	mgr.refreshTTLs()

	if ttl := sharedState.getTTL("calls:active:count"); ttl != SlotKeyTTL {
		t.Fatalf("expected global TTL %s, got %s", SlotKeyTTL, ttl)
	}
	if ttl := sharedState.getTTL("calls:active:count:replica-1"); ttl != SlotKeyTTL {
		t.Fatalf("expected replica TTL %s, got %s", SlotKeyTTL, ttl)
	}
	if ttl := sharedState.getTTL(workspaceSlotsKey("replica-1")); ttl != SlotKeyTTL {
		t.Fatalf("expected replica workspace hash TTL %s, got %s", SlotKeyTTL, ttl)
	}
	if ttl := sharedState.getTTL(workspaceCallKey("ws-1")); ttl != SlotKeyTTL {
		t.Fatalf("expected workspace TTL %s, got %s", SlotKeyTTL, ttl)
	}
}
