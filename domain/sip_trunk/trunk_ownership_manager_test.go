package sip_trunk

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/cache"
)

type mockSharedState struct {
	mu          sync.Mutex
	strings     map[string]string
	sets        map[string]map[string]bool
	hashes      map[string]map[string]string
	counters    map[string]int64
	ttls        map[string]time.Duration
	expireCalls map[string]int

	errOnSetNX error
	errOnGet   map[string]error
	errOnDel   error
}

func newMockSharedState() *mockSharedState {
	return &mockSharedState{
		strings:     make(map[string]string),
		sets:        make(map[string]map[string]bool),
		hashes:      make(map[string]map[string]string),
		counters:    make(map[string]int64),
		ttls:        make(map[string]time.Duration),
		expireCalls: make(map[string]int),
		errOnGet:    make(map[string]error),
	}
}

func (m *mockSharedState) SetNX(key, value string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.errOnSetNX != nil {
		return false, m.errOnSetNX
	}
	if _, ok := m.strings[key]; ok {
		return false, nil
	}
	m.strings[key] = value
	if ttl > 0 {
		m.ttls[key] = ttl
	}
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

func (m *mockSharedState) forceSet(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.strings[key] = value
}

func (m *mockSharedState) GetString(key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.errOnGet[key]; ok {
		return "", err
	}
	return m.strings[key], nil
}

func (m *mockSharedState) Del(keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.errOnDel != nil {
		return m.errOnDel
	}
	for _, k := range keys {
		delete(m.strings, k)
		delete(m.sets, k)
		delete(m.hashes, k)
		delete(m.counters, k)
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
	if _, ok := m.sets[key]; ok {
		return true, nil
	}
	return false, nil
}

func (m *mockSharedState) Expire(key string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ttls[key] = ttl
	m.expireCalls[key]++
	_, exists := m.strings[key]
	return exists, nil
}

func (m *mockSharedState) SAdd(key string, members ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sets[key] == nil {
		m.sets[key] = make(map[string]bool)
	}
	for _, mem := range members {
		m.sets[key][mem] = true
	}
	return nil
}

func (m *mockSharedState) SRem(key string, members ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sets[key] == nil {
		return nil
	}
	for _, mem := range members {
		delete(m.sets[key], mem)
	}
	return nil
}

func (m *mockSharedState) SMembers(key string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sets[key]
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	return out, nil
}

func (m *mockSharedState) Incr(key string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[key]++
	return m.counters[key], nil
}

func (m *mockSharedState) Decr(key string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[key]--
	return m.counters[key], nil
}

func (m *mockSharedState) IncrWithTTL(key string, _ time.Duration) (int64, error) {
	return m.Incr(key)
}

func (m *mockSharedState) TryIncr(key string, max int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.counters[key] >= max {
		return false, nil
	}
	m.counters[key]++
	return true, nil
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

func (m *mockSharedState) TryIncrBy(key string, delta, max int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.counters[key]+delta > max {
		return false, nil
	}
	m.counters[key] += delta
	return true, nil
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
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[k] = v
	}
	return out, nil
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

var _ cache.SharedState = (*mockSharedState)(nil)

type callbackRecorder struct {
	mu       sync.Mutex
	acquired []string
	lost     []string
	acquireN atomic.Int32
	lostN    atomic.Int32
}

func (r *callbackRecorder) onAcquired(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.acquired = append(r.acquired, id)
	r.acquireN.Add(1)
}

func (r *callbackRecorder) onLost(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lost = append(r.lost, id)
	r.lostN.Add(1)
}

func (r *callbackRecorder) acquiredIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.acquired))
	copy(out, r.acquired)
	return out
}

func (r *callbackRecorder) lostIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.lost))
	copy(out, r.lost)
	return out
}

func newTestOwnership(replicaID string) (*TrunkOwnershipManager, *mockSharedState, *callbackRecorder) {
	shared := newMockSharedState()
	mgr := NewTrunkOwnershipManager(shared, replicaID)
	rec := &callbackRecorder{}
	mgr.SetCallbacks(OwnershipCallbacks{
		OnAcquired: rec.onAcquired,
		OnLost:     rec.onLost,
	})
	return mgr, shared, rec
}

func waitFor(t *testing.T, cond func() bool, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("waitFor timed out: %s", msg)
}

func TestTryAcquire_NoExistingOwner_BecomesOwner(t *testing.T) {
	mgr, shared, rec := newTestOwnership("replica-A")

	ok, err := mgr.TryAcquire("trunk-1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !ok {
		t.Fatal("expected acquire to succeed")
	}
	if !mgr.IsOwner("trunk-1") {
		t.Fatal("manager should report owner=true")
	}
	v, _ := shared.GetString(trunkOwnerKey("trunk-1"))
	if v != "replica-A" {
		t.Fatalf("redis key value = %q; want replica-A", v)
	}
	if got := rec.acquireN.Load(); got != 1 {
		t.Fatalf("OnAcquired fired %d times; want 1", got)
	}
}

func TestTryAcquire_AlreadyOwnedBySelf_RefreshesTTLNoCallback(t *testing.T) {
	mgr, shared, rec := newTestOwnership("replica-A")

	if _, err := mgr.TryAcquire("trunk-1"); err != nil {
		t.Fatal(err)
	}
	rec.acquireN.Store(0)

	ok, err := mgr.TryAcquire("trunk-1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !ok {
		t.Fatal("re-acquire should still report ownership")
	}
	if got := rec.acquireN.Load(); got != 0 {
		t.Fatalf("OnAcquired should NOT fire on re-acquire; got %d calls", got)
	}

	shared.mu.Lock()
	calls := shared.expireCalls[trunkOwnerKey("trunk-1")]
	shared.mu.Unlock()
	if calls < 1 {
		t.Fatalf("expected at least 1 Expire refresh; got %d", calls)
	}
}

func TestTryAcquire_OwnedByOther_Fails(t *testing.T) {
	mgr, shared, rec := newTestOwnership("replica-A")
	shared.forceSet(trunkOwnerKey("trunk-1"), "replica-B")

	ok, err := mgr.TryAcquire("trunk-1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok {
		t.Fatal("expected acquire to fail when held by other replica")
	}
	if mgr.IsOwner("trunk-1") {
		t.Fatal("manager should not report ownership")
	}
	if got := rec.acquireN.Load(); got != 0 {
		t.Fatalf("OnAcquired must not fire; got %d", got)
	}
}

func TestTryAcquire_PersistsTrunkInReplicaSet(t *testing.T) {
	mgr, shared, _ := newTestOwnership("replica-A")
	if _, err := mgr.TryAcquire("trunk-1"); err != nil {
		t.Fatal(err)
	}
	members, _ := shared.SMembers(replicaTrunksKey("replica-A"))
	if len(members) != 1 || members[0] != "trunk-1" {
		t.Fatalf("replica set members = %v; want [trunk-1]", members)
	}
}

func TestTryAcquire_RedisError_Propagates(t *testing.T) {
	mgr, shared, rec := newTestOwnership("replica-A")
	shared.errOnSetNX = errors.New("redis down")

	ok, err := mgr.TryAcquire("trunk-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if ok {
		t.Fatal("expected ok=false on error")
	}
	if got := rec.acquireN.Load(); got != 0 {
		t.Fatalf("OnAcquired must not fire on error; got %d", got)
	}
}

func TestRelease_OwnedBySelf_DeletesKeyAndCallsOnLost(t *testing.T) {
	mgr, shared, rec := newTestOwnership("replica-A")
	if _, err := mgr.TryAcquire("trunk-1"); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Release("trunk-1"); err != nil {
		t.Fatal(err)
	}
	if mgr.IsOwner("trunk-1") {
		t.Fatal("should not own after Release")
	}
	v, _ := shared.GetString(trunkOwnerKey("trunk-1"))
	if v != "" {
		t.Fatalf("expected key deleted; got %q", v)
	}
	if got := rec.lostN.Load(); got != 1 {
		t.Fatalf("OnLost calls = %d; want 1", got)
	}
}

func TestRelease_NotOwned_NoOp(t *testing.T) {
	mgr, _, rec := newTestOwnership("replica-A")
	if err := mgr.Release("trunk-not-mine"); err != nil {
		t.Fatal(err)
	}
	if got := rec.lostN.Load(); got != 0 {
		t.Fatalf("OnLost must not fire; got %d", got)
	}
}

func TestRelease_DoesNotDeleteKeyOwnedByOther(t *testing.T) {

	mgr, shared, _ := newTestOwnership("replica-A")
	if _, err := mgr.TryAcquire("trunk-1"); err != nil {
		t.Fatal(err)
	}
	shared.forceSet(trunkOwnerKey("trunk-1"), "replica-B")

	if err := mgr.Release("trunk-1"); err != nil {
		t.Fatal(err)
	}
	v, _ := shared.GetString(trunkOwnerKey("trunk-1"))
	if v != "replica-B" {
		t.Fatalf("Release stomped on other replica's lock: got %q want replica-B", v)
	}
}

func TestFindOwner_ReturnsCurrentOwner(t *testing.T) {
	mgr, shared, _ := newTestOwnership("replica-A")
	shared.forceSet(trunkOwnerKey("trunk-9"), "replica-X")

	owner, err := mgr.FindOwner("trunk-9")
	if err != nil {
		t.Fatal(err)
	}
	if owner != "replica-X" {
		t.Fatalf("owner = %q; want replica-X", owner)
	}
}

func TestFindOwner_NoOwner_ReturnsEmpty(t *testing.T) {
	mgr, _, _ := newTestOwnership("replica-A")
	owner, err := mgr.FindOwner("trunk-orphan")
	if err != nil {
		t.Fatal(err)
	}
	if owner != "" {
		t.Fatalf("owner = %q; want empty", owner)
	}
}

func TestFindOwnerAddress_NoOwner_ReturnsEmptyStrings(t *testing.T) {
	mgr, _, _ := newTestOwnership("replica-A")
	rid, addr, err := mgr.FindOwnerAddress("trunk-orphan")
	if err != nil {
		t.Fatal(err)
	}
	if rid != "" || addr != "" {
		t.Fatalf("got (%q,%q); want empty", rid, addr)
	}
}

func TestFindOwnerAddress_OwnerWithoutHeartbeat_ReturnsReplicaIDOnly(t *testing.T) {
	mgr, shared, _ := newTestOwnership("replica-A")
	shared.forceSet(trunkOwnerKey("trunk-9"), "replica-X")

	rid, addr, err := mgr.FindOwnerAddress("trunk-9")
	if err != nil {
		t.Fatal(err)
	}
	if rid != "replica-X" {
		t.Fatalf("rid = %q; want replica-X", rid)
	}
	if addr != "" {
		t.Fatalf("addr = %q; want empty (no heartbeat)", addr)
	}
}

func TestFindOwnerAddress_OwnerWithHeartbeat_ReturnsBoth(t *testing.T) {
	mgr, shared, _ := newTestOwnership("replica-A")
	shared.forceSet(trunkOwnerKey("trunk-9"), "replica-X")
	shared.forceSet("replica:heartbeat:replica-X", "https://vozko-3.example.com")

	rid, addr, err := mgr.FindOwnerAddress("trunk-9")
	if err != nil {
		t.Fatal(err)
	}
	if rid != "replica-X" {
		t.Fatalf("rid = %q; want replica-X", rid)
	}
	if addr != "https://vozko-3.example.com" {
		t.Fatalf("addr = %q; want https://vozko-3.example.com", addr)
	}
}

func TestRunHeartbeat_RefreshesOwnedTTLs(t *testing.T) {
	mgr, shared, _ := newTestOwnership("replica-A")
	if _, err := mgr.TryAcquire("trunk-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.TryAcquire("trunk-2"); err != nil {
		t.Fatal(err)
	}

	mgr.refreshAllLocks()

	shared.mu.Lock()
	c1 := shared.expireCalls[trunkOwnerKey("trunk-1")]
	c2 := shared.expireCalls[trunkOwnerKey("trunk-2")]
	shared.mu.Unlock()
	if c1 < 1 || c2 < 1 {
		t.Fatalf("expected Expire on both trunks; got c1=%d c2=%d", c1, c2)
	}
}

func TestRunHeartbeat_LockStolen_FiresOnLostAndDropsOwnership(t *testing.T) {
	mgr, shared, rec := newTestOwnership("replica-A")
	if _, err := mgr.TryAcquire("trunk-1"); err != nil {
		t.Fatal(err)
	}

	shared.forceSet(trunkOwnerKey("trunk-1"), "replica-B")

	mgr.refreshAllLocks()

	if mgr.IsOwner("trunk-1") {
		t.Fatal("should have dropped ownership after lock theft")
	}
	if got := rec.lostN.Load(); got != 1 {
		t.Fatalf("OnLost calls = %d; want 1", got)
	}
}

func TestRunHeartbeat_StopsOnContextCancel(t *testing.T) {
	mgr, _, _ := newTestOwnership("replica-A")
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		mgr.RunHeartbeat(ctx)
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunHeartbeat did not return after context cancel")
	}
}

func TestTakeover_AcquiresOrphanedTrunks(t *testing.T) {
	mgr, _, rec := newTestOwnership("replica-A")
	enabled := []*SIPTrunk{
		{ID: "trunk-1", Enabled: true},
		{ID: "trunk-2", Enabled: true},
	}
	mgr.takeoverPass(func() ([]*SIPTrunk, error) { return enabled, nil })

	if !mgr.IsOwner("trunk-1") || !mgr.IsOwner("trunk-2") {
		t.Fatalf("expected to own both trunks; owned=%v", mgr.OwnedSnapshot())
	}
	if got := rec.acquireN.Load(); got != 2 {
		t.Fatalf("OnAcquired calls = %d; want 2", got)
	}
}

func TestTakeover_SkipsTrunksOwnedByOtherReplicas(t *testing.T) {
	mgr, shared, rec := newTestOwnership("replica-A")
	shared.forceSet(trunkOwnerKey("trunk-2"), "replica-B")
	enabled := []*SIPTrunk{
		{ID: "trunk-1", Enabled: true},
		{ID: "trunk-2", Enabled: true},
	}

	mgr.takeoverPass(func() ([]*SIPTrunk, error) { return enabled, nil })

	if !mgr.IsOwner("trunk-1") {
		t.Fatal("should own orphan trunk-1")
	}
	if mgr.IsOwner("trunk-2") {
		t.Fatal("should not steal trunk-2 from replica-B")
	}
	if got := rec.acquireN.Load(); got != 1 {
		t.Fatalf("OnAcquired calls = %d; want 1", got)
	}
}

func TestTakeover_AlreadyOwned_NoDoubleCallback(t *testing.T) {
	mgr, _, rec := newTestOwnership("replica-A")
	if _, err := mgr.TryAcquire("trunk-1"); err != nil {
		t.Fatal(err)
	}
	rec.acquireN.Store(0)

	enabled := []*SIPTrunk{{ID: "trunk-1", Enabled: true}}
	mgr.takeoverPass(func() ([]*SIPTrunk, error) { return enabled, nil })

	if got := rec.acquireN.Load(); got != 0 {
		t.Fatalf("OnAcquired must not refire on already-owned trunk; got %d", got)
	}
}

func TestTakeover_StopsOnContextCancel(t *testing.T) {
	mgr, _, _ := newTestOwnership("replica-A")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		mgr.RunTakeover(ctx, func() ([]*SIPTrunk, error) { return nil, nil })
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunTakeover did not return after context cancel")
	}
}

func TestOwnedSnapshot_ReflectsAcquireAndRelease(t *testing.T) {
	mgr, _, _ := newTestOwnership("replica-A")
	_, _ = mgr.TryAcquire("trunk-1")
	_, _ = mgr.TryAcquire("trunk-2")

	got := mgr.OwnedSnapshot()
	if len(got) != 2 {
		t.Fatalf("snapshot len = %d; want 2 (%v)", len(got), got)
	}

	_ = mgr.Release("trunk-1")
	got = mgr.OwnedSnapshot()
	if len(got) != 1 || got[0] != "trunk-2" {
		t.Fatalf("snapshot after release = %v; want [trunk-2]", got)
	}
}
