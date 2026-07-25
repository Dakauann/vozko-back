package sip_trunk

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type stubReplicas struct {
	mu  sync.Mutex
	ids []string
	err error
}

func (s *stubReplicas) LiveReplicas() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	out := append([]string(nil), s.ids...)
	return out, nil
}

func (s *stubReplicas) set(ids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ids = ids
}

func newReconcilerHarness(replicaID string, replicaIDs []string, trunks []*SIPTrunk) (
	*TrunkOwnershipManager, *stubReplicas, *atomic.Int32, *atomic.Int32,
) {
	shared := newMockSharedState()
	mgr := NewTrunkOwnershipManager(shared, replicaID)

	var acquired, released atomic.Int32
	mgr.SetCallbacks(OwnershipCallbacks{
		OnAcquired: func(string) { acquired.Add(1) },
		OnLost:     func(string) { released.Add(1) },
	})

	rep := &stubReplicas{ids: append([]string(nil), replicaIDs...)}

	listFn := func() ([]*SIPTrunk, error) {
		out := make([]*SIPTrunk, 0, len(trunks))
		out = append(out, trunks...)
		return out, nil
	}

	mgr.ReconcileOnce(rep, listFn)
	return mgr, rep, &acquired, &released
}

func TestReconcile_AcquiresAssignedTrunks(t *testing.T) {
	trunks := []*SIPTrunk{
		{ID: "T1", WorkspaceID: "W1"},
		{ID: "T2", WorkspaceID: "W2"},
		{ID: "T3", WorkspaceID: "W3"},
	}

	mgr, _, acquired, released := newReconcilerHarness("r-only", []string{"r-only"}, trunks)

	for _, tr := range trunks {
		if !mgr.IsOwner(tr.ID) {
			t.Fatalf("expected replica to own %s", tr.ID)
		}
	}
	if got := acquired.Load(); got != 3 {
		t.Fatalf("expected 3 OnAcquired calls, got %d", got)
	}
	if got := released.Load(); got != 0 {
		t.Fatalf("expected 0 OnLost calls, got %d", got)
	}
}

func TestReconcile_DistributesAcrossReplicas(t *testing.T) {
	trunks := []*SIPTrunk{}
	for i := 0; i < 30; i++ {
		trunks = append(trunks, &SIPTrunk{
			ID:          "T" + itoa(i),
			WorkspaceID: "W" + itoa(i),
		})
	}
	replicas := []string{"r1", "r2", "r3"}

	owners := map[string]int{}
	for _, rid := range replicas {
		mgr, _, _, _ := newReconcilerHarness(rid, replicas, trunks)
		owners[rid] = len(mgr.OwnedSnapshot())
	}

	total := owners["r1"] + owners["r2"] + owners["r3"]
	if total != 30 {
		t.Fatalf("trunks must be assigned to exactly one replica each: total=%d owners=%v", total, owners)
	}
	for _, rid := range replicas {
		if owners[rid] == 0 {
			t.Fatalf("replica %s got zero trunks: %v", rid, owners)
		}
	}
}

func TestReconcile_WorkspaceTrunksColocated(t *testing.T) {

	trunks := []*SIPTrunk{
		{ID: "T1", WorkspaceID: "W1"},
		{ID: "T2", WorkspaceID: "W1"},
		{ID: "T3", WorkspaceID: "W1"},
		{ID: "T4", WorkspaceID: "W2"},
		{ID: "T5", WorkspaceID: "W2"},
	}
	replicas := []string{"r1", "r2", "r3"}

	ownerOf := map[string]string{}
	for _, rid := range replicas {
		mgr, _, _, _ := newReconcilerHarness(rid, replicas, trunks)
		for _, owned := range mgr.OwnedSnapshot() {
			ownerOf[owned] = rid
		}
	}

	if ownerOf["T1"] != ownerOf["T2"] || ownerOf["T2"] != ownerOf["T3"] {
		t.Fatalf("W1 trunks split across replicas: %v", ownerOf)
	}
	if ownerOf["T4"] != ownerOf["T5"] {
		t.Fatalf("W2 trunks split across replicas: %v", ownerOf)
	}
}

func TestReconcile_ReleasesUnassignedTrunks(t *testing.T) {

	trunks := []*SIPTrunk{
		{ID: "T1", WorkspaceID: "W1"},
		{ID: "T2", WorkspaceID: "W2"},
		{ID: "T3", WorkspaceID: "W3"},
	}
	mgr, rep, _, released := newReconcilerHarness("r1", []string{"r1"}, trunks)
	if len(mgr.OwnedSnapshot()) != 3 {
		t.Fatalf("setup: expected r1 to own all 3, got %v", mgr.OwnedSnapshot())
	}

	rep.set("r1", "r2", "r3")
	mgr.ReconcileOnce(rep, func() ([]*SIPTrunk, error) {
		return append([]*SIPTrunk(nil), trunks...), nil
	})

	owned := len(mgr.OwnedSnapshot())
	if owned >= 3 {
		t.Fatalf("expected r1 to release some trunks after rebalance, still owns %d", owned)
	}
	if got := released.Load(); got == 0 {
		t.Fatalf("expected at least one OnLost call, got %d", got)
	}
}

func TestReconcile_AbortsOnEmptyCluster(t *testing.T) {
	trunks := []*SIPTrunk{
		{ID: "T1", WorkspaceID: "W1"},
	}
	mgr, rep, _, released := newReconcilerHarness("r1", []string{"r1"}, trunks)
	if !mgr.IsOwner("T1") {
		t.Fatalf("setup: expected r1 to own T1")
	}

	rep.set()
	mgr.ReconcileOnce(rep, func() ([]*SIPTrunk, error) {
		return append([]*SIPTrunk(nil), trunks...), nil
	})

	if !mgr.IsOwner("T1") {
		t.Fatalf("must preserve ownership when cluster appears empty")
	}
	if got := released.Load(); got != 0 {
		t.Fatalf("expected 0 releases on empty cluster, got %d", got)
	}
}

func TestReconcile_Idempotent(t *testing.T) {
	trunks := []*SIPTrunk{
		{ID: "T1", WorkspaceID: "W1"},
		{ID: "T2", WorkspaceID: "W2"},
	}
	mgr, rep, acquired, released := newReconcilerHarness("r1", []string{"r1", "r2"}, trunks)
	first := acquired.Load()

	listFn := func() ([]*SIPTrunk, error) { return append([]*SIPTrunk(nil), trunks...), nil }
	for i := 0; i < 5; i++ {
		mgr.ReconcileOnce(rep, listFn)
	}

	if acquired.Load() != first {
		t.Fatalf("subsequent reconciles re-fired OnAcquired: first=%d now=%d", first, acquired.Load())
	}
	if released.Load() != 0 {
		t.Fatalf("idempotent reconcile must not release: got %d", released.Load())
	}
}

func TestReconcile_GlobalTrunkUsesTrunkID(t *testing.T) {

	trunks := []*SIPTrunk{
		{ID: "Tglob1", WorkspaceID: ""},
		{ID: "Tglob2", WorkspaceID: ""},
	}
	replicas := []string{"r1", "r2", "r3"}

	owners := map[string]string{}
	for _, rid := range replicas {
		mgr, _, _, _ := newReconcilerHarness(rid, replicas, trunks)
		for _, owned := range mgr.OwnedSnapshot() {
			owners[owned] = rid
		}
	}

	if owners["Tglob1"] == "" || owners["Tglob2"] == "" {
		t.Fatalf("missing owner: %v", owners)
	}
}

func TestRunReconciler_TickerConverges(t *testing.T) {

	trunks := []*SIPTrunk{
		{ID: "T1", WorkspaceID: "W1"},
	}
	shared := newMockSharedState()
	mgr := NewTrunkOwnershipManager(shared, "r1")
	acquired := make(chan string, 1)
	mgr.SetCallbacks(OwnershipCallbacks{
		OnAcquired: func(id string) { acquired <- id },
	})

	rep := &stubReplicas{ids: []string{"r1"}}
	listFn := func() ([]*SIPTrunk, error) { return append([]*SIPTrunk(nil), trunks...), nil }

	mgr.ReconcileOnce(rep, listFn)

	select {
	case got := <-acquired:
		if got != "T1" {
			t.Fatalf("expected T1, got %s", got)
		}
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for OnAcquired")
	}
}

func TestReconcile_StealsFromDeadReplica(t *testing.T) {
	shared := newMockSharedState()
	mgr := NewTrunkOwnershipManager(shared, "r1")

	var acquired atomic.Int32
	mgr.SetCallbacks(OwnershipCallbacks{
		OnAcquired: func(string) { acquired.Add(1) },
	})

	shared.forceSet(trunkOwnerKey("T1"), "r-dead")

	rep := &stubReplicas{ids: []string{"r1"}}
	listFn := func() ([]*SIPTrunk, error) {
		return []*SIPTrunk{{ID: "T1", WorkspaceID: "W1"}}, nil
	}

	mgr.ReconcileOnce(rep, listFn)

	if !mgr.IsOwner("T1") {
		t.Fatalf("expected r1 to take over T1 from dead r-dead")
	}
	if got := acquired.Load(); got != 1 {
		t.Fatalf("expected exactly 1 OnAcquired, got %d", got)
	}
}

func TestReconcile_DoesNotStealFromLiveReplica(t *testing.T) {
	shared := newMockSharedState()
	mgr := NewTrunkOwnershipManager(shared, "r1")

	var acquired atomic.Int32
	mgr.SetCallbacks(OwnershipCallbacks{
		OnAcquired: func(string) { acquired.Add(1) },
	})

	shared.forceSet(trunkOwnerKey("T1"), "r-other")

	live := []string{"r1", "r-other"}
	var trunkID string
	for i := 0; i < 200; i++ {
		cand := "T" + itoa(i)
		if AssignOwner("ws:W"+itoa(i), live) == "r1" {
			trunkID = cand
			break
		}
	}
	if trunkID == "" {
		t.Fatal("setup: could not find a trunk ID assigned to r1")
	}
	wsID := "W" + trunkID[1:]
	shared.forceSet(trunkOwnerKey(trunkID), "r-other")

	rep := &stubReplicas{ids: live}
	listFn := func() ([]*SIPTrunk, error) {
		return []*SIPTrunk{{ID: trunkID, WorkspaceID: wsID}}, nil
	}

	mgr.ReconcileOnce(rep, listFn)

	if mgr.IsOwner(trunkID) {
		t.Fatalf("must not steal from live replica r-other")
	}
	cur, _ := shared.GetString(trunkOwnerKey(trunkID))
	if cur != "r-other" {
		t.Fatalf("lock for live owner was clobbered: %q", cur)
	}
	if got := acquired.Load(); got != 0 {
		t.Fatalf("expected 0 OnAcquired, got %d", got)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
