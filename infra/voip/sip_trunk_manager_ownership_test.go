package voipinfra

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/cache"
	"vozko/domain/sip_trunk"
)

type ownTestSharedState struct {
	mu       sync.Mutex
	strings  map[string]string
	sets     map[string]map[string]bool
	hashes   map[string]map[string]string
	counters map[string]int64
	ttls     map[string]time.Duration

	errOnSetNX error
}

func newOwnTestSharedState() *ownTestSharedState {
	return &ownTestSharedState{
		strings:  map[string]string{},
		sets:     map[string]map[string]bool{},
		hashes:   map[string]map[string]string{},
		counters: map[string]int64{},
		ttls:     map[string]time.Duration{},
	}
}

func (s *ownTestSharedState) forceSet(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.strings[key] = value
}

func (s *ownTestSharedState) SetNX(key, value string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.errOnSetNX != nil {
		return false, s.errOnSetNX
	}
	if _, ok := s.strings[key]; ok {
		return false, nil
	}
	s.strings[key] = value
	if ttl > 0 {
		s.ttls[key] = ttl
	}
	return true, nil
}

func (s *ownTestSharedState) SetString(key, value string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.strings[key] = value
	if ttl > 0 {
		s.ttls[key] = ttl
	}
	return nil
}

func (s *ownTestSharedState) GetString(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.strings[key], nil
}

func (s *ownTestSharedState) Del(keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		delete(s.strings, k)
		delete(s.sets, k)
		delete(s.ttls, k)
	}
	return nil
}

func (s *ownTestSharedState) Exists(key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.strings[key]
	return ok, nil
}

func (s *ownTestSharedState) Expire(key string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ttls[key] = ttl
	_, ok := s.strings[key]
	return ok, nil
}

func (s *ownTestSharedState) SAdd(key string, members ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sets[key] == nil {
		s.sets[key] = map[string]bool{}
	}
	for _, m := range members {
		s.sets[key][m] = true
	}
	return nil
}

func (s *ownTestSharedState) SRem(key string, members ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sets[key] == nil {
		return nil
	}
	for _, m := range members {
		delete(s.sets[key], m)
	}
	return nil
}

func (s *ownTestSharedState) SMembers(key string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.sets[key]))
	for m := range s.sets[key] {
		out = append(out, m)
	}
	return out, nil
}

func (s *ownTestSharedState) Incr(key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counters[key]++
	return s.counters[key], nil
}
func (s *ownTestSharedState) Decr(key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counters[key]--
	return s.counters[key], nil
}
func (s *ownTestSharedState) IncrWithTTL(k string, _ time.Duration) (int64, error) {
	return s.Incr(k)
}
func (s *ownTestSharedState) TryIncr(key string, max int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.counters[key] >= max {
		return false, nil
	}
	s.counters[key]++
	return true, nil
}
func (s *ownTestSharedState) IncrBy(key string, amount int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counters[key] += amount
	return s.counters[key], nil
}
func (s *ownTestSharedState) DecrBy(key string, amount int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counters[key] -= amount
	return s.counters[key], nil
}
func (s *ownTestSharedState) TryIncrBy(key string, delta, max int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.counters[key]+delta > max {
		return false, nil
	}
	s.counters[key] += delta
	return true, nil
}

func (s *ownTestSharedState) Publish(string, []byte) error                    { return nil }
func (s *ownTestSharedState) Subscribe(context.Context, string, func([]byte)) {}

func (s *ownTestSharedState) HSet(key, field, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hashes[key] == nil {
		s.hashes[key] = map[string]string{}
	}
	s.hashes[key][field] = value
	return nil
}
func (s *ownTestSharedState) HDel(key, field string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hashes[key] != nil {
		delete(s.hashes[key], field)
	}
	return nil
}
func (s *ownTestSharedState) HGetAll(key string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for k, v := range s.hashes[key] {
		out[k] = v
	}
	return out, nil
}
func (s *ownTestSharedState) HIncrBy(key, field string, incr int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hashes[key] == nil {
		s.hashes[key] = map[string]string{}
	}
	var cur int64
	if raw, ok := s.hashes[key][field]; ok {
		fmt.Sscanf(raw, "%d", &cur)
	}
	cur += incr
	s.hashes[key][field] = fmt.Sprintf("%d", cur)
	return cur, nil
}

var _ cache.SharedState = (*ownTestSharedState)(nil)

type fakeTrunkRepo struct {
	mu              sync.Mutex
	byID            map[string]*sip_trunk.SIPTrunk
	findByIDErr     error
	updateStatusLog []sip_trunk.SIPTrunkStatusUpdate
}

func newFakeTrunkRepo(trunks ...*sip_trunk.SIPTrunk) *fakeTrunkRepo {
	r := &fakeTrunkRepo{byID: map[string]*sip_trunk.SIPTrunk{}}
	for _, t := range trunks {
		r.byID[t.ID] = t
	}
	return r
}

func (r *fakeTrunkRepo) Create(t *sip_trunk.SIPTrunk) error { r.byID[t.ID] = t; return nil }
func (r *fakeTrunkRepo) Update(t *sip_trunk.SIPTrunk) error { r.byID[t.ID] = t; return nil }
func (r *fakeTrunkRepo) Delete(id string) error             { delete(r.byID, id); return nil }
func (r *fakeTrunkRepo) FindByID(id string) (*sip_trunk.SIPTrunk, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.findByIDErr != nil {
		return nil, r.findByIDErr
	}
	t, ok := r.byID[id]
	if !ok {
		return nil, sip_trunk.ErrTrunkNotFound
	}
	return t, nil
}
func (r *fakeTrunkRepo) FindByIDs(ids []string) ([]*sip_trunk.SIPTrunk, error) {
	out := []*sip_trunk.SIPTrunk{}
	for _, id := range ids {
		if t, ok := r.byID[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *fakeTrunkRepo) FindAll(_, _ int) ([]*sip_trunk.SIPTrunk, int64, error) {
	out := []*sip_trunk.SIPTrunk{}
	for _, t := range r.byID {
		out = append(out, t)
	}
	return out, int64(len(out)), nil
}
func (r *fakeTrunkRepo) FindAccessible(_ string, _ []string, _, _ int) ([]*sip_trunk.SIPTrunk, int64, error) {
	return r.FindAll(0, 0)
}
func (r *fakeTrunkRepo) CountByWorkspace(workspaceID string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for _, t := range r.byID {
		if t.WorkspaceID == workspaceID {
			n++
		}
	}
	return n, nil
}
func (r *fakeTrunkRepo) FindEnabled() ([]*sip_trunk.SIPTrunk, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []*sip_trunk.SIPTrunk{}
	for _, t := range r.byID {
		if t.Enabled {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *fakeTrunkRepo) UpdateStatus(id string, status sip_trunk.RegistrationStatus, lastError string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateStatusLog = append(r.updateStatusLog, sip_trunk.SIPTrunkStatusUpdate{
		TrunkID: id, Status: status, Error: lastError,
	})
	if t, ok := r.byID[id]; ok {
		t.RegistrationStatus = status
		t.LastError = lastError
	}
	return nil
}

type lifecycleRecorder struct {
	mu              sync.Mutex
	registerCalls   []string
	unregisterCalls []string
	registerN       atomic.Int32
	unregisterN     atomic.Int32

	registerErr   error
	unregisterErr error
}

func (r *lifecycleRecorder) register(t *sip_trunk.SIPTrunk) error {
	r.mu.Lock()
	r.registerCalls = append(r.registerCalls, t.ID)
	r.mu.Unlock()
	r.registerN.Add(1)
	return r.registerErr
}

func (r *lifecycleRecorder) unregister(id string) error {
	r.mu.Lock()
	r.unregisterCalls = append(r.unregisterCalls, id)
	r.mu.Unlock()
	r.unregisterN.Add(1)
	return r.unregisterErr
}

func (r *lifecycleRecorder) registerIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.registerCalls))
	copy(out, r.registerCalls)
	return out
}

func (r *lifecycleRecorder) unregisterIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.unregisterCalls))
	copy(out, r.unregisterCalls)
	return out
}

type testHarness struct {
	t         *testing.T
	mgr       *SIPTrunkManager
	ownership *sip_trunk.TrunkOwnershipManager
	shared    *ownTestSharedState
	repo      *fakeTrunkRepo
	lifecycle *lifecycleRecorder
}

var uniqueSIPPortBase atomic.Int32

func init() { uniqueSIPPortBase.Store(45000) }

func newTestHarness(t *testing.T, replicaID string, trunks ...*sip_trunk.SIPTrunk) *testHarness {
	t.Helper()

	shared := newOwnTestSharedState()
	ownership := sip_trunk.NewTrunkOwnershipManager(shared, replicaID)
	repo := newFakeTrunkRepo(trunks...)
	rec := &lifecycleRecorder{}

	sipBase := int(uniqueSIPPortBase.Add(50))

	cfg := TrunkManagerConfig{
		SIPPortStart: sipBase,
		SIPPortCount: 10,
		RTPPortStart: 40000,
		RTPPortEnd:   40100,
		Ownership:    ownership,
	}
	mgr, err := NewSIPTrunkManager(cfg, repo)
	if err != nil {
		t.Fatalf("NewSIPTrunkManager: %v", err)
	}

	mgr.registerFn = rec.register
	mgr.unregisterFn = rec.unregister

	return &testHarness{
		t:         t,
		mgr:       mgr,
		ownership: ownership,
		shared:    shared,
		repo:      repo,
		lifecycle: rec,
	}
}

func (h *testHarness) wireOwnershipCallbacks() {

	h.ownership.SetCallbacks(sip_trunk.OwnershipCallbacks{
		OnAcquired: h.mgr.handleOwnershipAcquired,
		OnLost:     h.mgr.handleOwnershipLost,
	})
}

func makeTrunk(id string, enabled bool) *sip_trunk.SIPTrunk {
	return &sip_trunk.SIPTrunk{
		ID:        id,
		Name:      "trunk-" + id,
		Host:      "sip.example.com",
		Port:      5060,
		Username:  "u",
		Password:  "p",
		Enabled:   enabled,
		Transport: "udp",
	}
}

func TestNewSIPTrunkManager_RequiresOwnership(t *testing.T) {
	cfg := TrunkManagerConfig{
		SIPPortStart: 46000,
		SIPPortCount: 1,
		RTPPortStart: 40000,
		RTPPortEnd:   40100,
		Ownership:    nil,
	}
	_, err := NewSIPTrunkManager(cfg, newFakeTrunkRepo())
	if err == nil {
		t.Fatal("expected error when Ownership is nil, got nil")
	}
	if !contains(err.Error(), "Ownership is required") {
		t.Fatalf("expected ownership-required error, got: %v", err)
	}
}

func TestRegisterTrunk_OwnershipAcquired_RegistersTrunk(t *testing.T) {
	tr := makeTrunk("t1", true)
	h := newTestHarness(t, "replica-A", tr)
	h.wireOwnershipCallbacks()

	if err := h.mgr.RegisterTrunk(tr); err != nil {
		t.Fatalf("RegisterTrunk: %v", err)
	}

	if got := h.lifecycle.registerN.Load(); got < 1 {
		t.Fatalf("registerFn calls = %d, want >= 1", got)
	}
	if !h.ownership.IsOwner("t1") {
		t.Fatal("expected replica to own trunk t1 after RegisterTrunk")
	}
}

func TestRegisterTrunk_OwnedByOtherReplica_ReturnsErrTrunkNotOwnedHere(t *testing.T) {
	tr := makeTrunk("t1", true)
	h := newTestHarness(t, "replica-A", tr)
	h.wireOwnershipCallbacks()

	h.shared.forceSet("sip:trunk_owner:t1", "replica-B")

	err := h.mgr.RegisterTrunk(tr)
	if !errors.Is(err, sip_trunk.ErrTrunkNotOwnedHere) {
		t.Fatalf("expected ErrTrunkNotOwnedHere, got %v", err)
	}
	if got := h.lifecycle.registerN.Load(); got != 0 {
		t.Fatalf("registerFn must not be called when ownership rejected, got %d calls", got)
	}
}

func TestRegisterTrunk_RegisterFnFails_ReleasesOwnership(t *testing.T) {
	tr := makeTrunk("t1", true)
	h := newTestHarness(t, "replica-A", tr)
	h.lifecycle.registerErr = errors.New("simulated SIP failure")
	h.wireOwnershipCallbacks()

	err := h.mgr.RegisterTrunk(tr)
	if err == nil {
		t.Fatal("expected error from RegisterTrunk, got nil")
	}
	if h.ownership.IsOwner("t1") {
		t.Fatal("ownership must be released when registerFn fails")
	}
}

func TestRegisterTrunk_NilTrunk_ReturnsError(t *testing.T) {
	h := newTestHarness(t, "replica-A")
	if err := h.mgr.RegisterTrunk(nil); err == nil {
		t.Fatal("expected error for nil trunk")
	}
}

func TestRegisterTrunk_TryAcquireRedisError_PropagatesError(t *testing.T) {
	tr := makeTrunk("t1", true)
	h := newTestHarness(t, "replica-A", tr)
	h.shared.errOnSetNX = errors.New("redis down")

	err := h.mgr.RegisterTrunk(tr)
	if err == nil || !contains(err.Error(), "redis down") {
		t.Fatalf("expected redis error to propagate, got %v", err)
	}
}

func TestHandleOwnershipAcquired_FetchesFromRepoAndCallsRegister(t *testing.T) {
	tr := makeTrunk("t1", true)
	h := newTestHarness(t, "replica-A", tr)

	h.mgr.handleOwnershipAcquired("t1")

	ids := h.lifecycle.registerIDs()
	if len(ids) != 1 || ids[0] != "t1" {
		t.Fatalf("expected registerFn to be called with t1, got %v", ids)
	}
}

func TestHandleOwnershipAcquired_DisabledTrunk_ReleasesAndDoesNotRegister(t *testing.T) {
	tr := makeTrunk("t1", false)
	h := newTestHarness(t, "replica-A", tr)

	if _, err := h.ownership.TryAcquire("t1"); err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}

	h.mgr.handleOwnershipAcquired("t1")

	if got := h.lifecycle.registerN.Load(); got != 0 {
		t.Fatalf("registerFn must not run for disabled trunk, got %d calls", got)
	}
	if h.ownership.IsOwner("t1") {
		t.Fatal("expected ownership released for disabled trunk")
	}
}

func TestHandleOwnershipAcquired_RepoError_ReleasesOwnership(t *testing.T) {
	h := newTestHarness(t, "replica-A")
	h.repo.findByIDErr = errors.New("db down")

	if _, err := h.ownership.TryAcquire("t1"); err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}

	h.mgr.handleOwnershipAcquired("t1")

	if h.ownership.IsOwner("t1") {
		t.Fatal("expected ownership released on repo error")
	}
}

func TestHandleOwnershipAcquired_RegisterFnFails_ReleasesOwnership(t *testing.T) {
	tr := makeTrunk("t1", true)
	h := newTestHarness(t, "replica-A", tr)
	h.lifecycle.registerErr = errors.New("sip register failed")

	if _, err := h.ownership.TryAcquire("t1"); err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}

	h.mgr.handleOwnershipAcquired("t1")

	if h.ownership.IsOwner("t1") {
		t.Fatal("expected ownership released on registerFn failure")
	}
}

func TestHandleOwnershipLost_CallsUnregisterFn(t *testing.T) {
	h := newTestHarness(t, "replica-A")

	h.mgr.handleOwnershipLost("t1")

	ids := h.lifecycle.unregisterIDs()
	if len(ids) != 1 || ids[0] != "t1" {
		t.Fatalf("expected unregisterFn called with t1, got %v", ids)
	}
}

func TestHandleOwnershipLost_TolerantOfTrunkNotFoundError(t *testing.T) {
	h := newTestHarness(t, "replica-A")
	h.lifecycle.unregisterErr = sip_trunk.ErrTrunkNotFound

	h.mgr.handleOwnershipLost("t-ghost")
}

func TestInvite_NotOwner_ReturnsErrTrunkNotOwnedHere(t *testing.T) {
	h := newTestHarness(t, "replica-A")

	_, err := h.mgr.Invite(context.Background(), "t1", sip_trunk.TrunkInviteInput{
		PhoneNumber: "+5511999999999",
	})
	if !errors.Is(err, sip_trunk.ErrTrunkNotOwnedHere) {
		t.Fatalf("expected ErrTrunkNotOwnedHere, got %v", err)
	}
}

func TestHangup_NotOwner_ReturnsErrTrunkNotOwnedHere(t *testing.T) {
	h := newTestHarness(t, "replica-A")

	err := h.mgr.Hangup(context.Background(), "t1", "call-1")
	if !errors.Is(err, sip_trunk.ErrTrunkNotOwnedHere) {
		t.Fatalf("expected ErrTrunkNotOwnedHere, got %v", err)
	}
}

func TestUnregisterTrunk_OwnedHere_ReleasesAndCallsUnregister(t *testing.T) {
	tr := makeTrunk("t1", true)
	h := newTestHarness(t, "replica-A", tr)
	h.wireOwnershipCallbacks()

	if err := h.mgr.RegisterTrunk(tr); err != nil {
		t.Fatalf("RegisterTrunk: %v", err)
	}
	if !h.ownership.IsOwner("t1") {
		t.Fatal("setup: expected ownership of t1")
	}

	if err := h.mgr.UnregisterTrunk("t1"); err != nil {
		t.Fatalf("UnregisterTrunk: %v", err)
	}

	if h.ownership.IsOwner("t1") {
		t.Fatal("expected ownership released after UnregisterTrunk")
	}
	ids := h.lifecycle.unregisterIDs()
	if len(ids) != 1 || ids[0] != "t1" {
		t.Fatalf("expected unregisterFn called once with t1, got %v", ids)
	}
}

func TestUnregisterTrunk_NotOwned_StillCallsUnregisterFn(t *testing.T) {
	h := newTestHarness(t, "replica-A")

	h.lifecycle.unregisterErr = sip_trunk.ErrTrunkNotFound

	err := h.mgr.UnregisterTrunk("t-nope")
	if !errors.Is(err, sip_trunk.ErrTrunkNotFound) {
		t.Fatalf("expected ErrTrunkNotFound bubbled up, got %v", err)
	}
	if got := h.lifecycle.unregisterN.Load(); got != 1 {
		t.Fatalf("expected unregisterFn called once even when not owned, got %d", got)
	}
}

func TestStop_ReleasesAllOwnedTrunksAndStopsBackgroundLoops(t *testing.T) {
	tr1 := makeTrunk("t1", true)
	tr2 := makeTrunk("t2", true)
	h := newTestHarness(t, "replica-A", tr1, tr2)

	if err := h.mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if _, err := h.ownership.TryAcquire("t1"); err != nil {
		t.Fatalf("TryAcquire t1: %v", err)
	}
	if _, err := h.ownership.TryAcquire("t2"); err != nil {
		t.Fatalf("TryAcquire t2: %v", err)
	}

	if err := h.mgr.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if owned := h.ownership.OwnedSnapshot(); len(owned) != 0 {
		t.Fatalf("expected all locks released on Stop, still owned: %v", owned)
	}

	if got := h.lifecycle.unregisterN.Load(); got < 2 {
		t.Fatalf("expected at least 2 unregister calls (one per trunk), got %d", got)
	}
}

func TestStart_RejectsDoubleStart(t *testing.T) {
	h := newTestHarness(t, "replica-A")
	if err := h.mgr.Start(context.Background()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := h.mgr.Start(context.Background()); err == nil {
		t.Fatal("expected error on double Start")
	}
	_ = h.mgr.Stop()
}

func waitForCondition(t *testing.T, cond func() bool, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", msg)
}
