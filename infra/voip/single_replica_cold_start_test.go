package voipinfra

import (
	"context"
	"strconv"
	"testing"
	"time"

	"vozko/domain/cluster"
	"vozko/domain/sip_trunk"
)

func TestSingleReplica_ColdStart_AcquiresTrunksImmediately(t *testing.T) {
	shared := newOwnTestSharedState()
	const replicaID = "r-only"

	if err := shared.SetString(cluster.HeartbeatKeyPrefix+replicaID, "1", 30*time.Second); err != nil {
		t.Fatalf("seed heartbeat: %v", err)
	}
	registry := cluster.NewRegistry(shared)
	if err := registry.Announce(replicaID); err != nil {
		t.Fatalf("announce: %v", err)
	}

	mgr := sip_trunk.NewTrunkOwnershipManager(shared, replicaID)

	trunks := []*sip_trunk.SIPTrunk{
		{ID: "T1", WorkspaceID: "W1", Enabled: true},
		{ID: "T2", WorkspaceID: "W2", Enabled: true},
		{ID: "T3", WorkspaceID: "", Enabled: true},
	}
	listFn := func() ([]*sip_trunk.SIPTrunk, error) {
		out := make([]*sip_trunk.SIPTrunk, len(trunks))
		copy(out, trunks)
		return out, nil
	}

	mgr.ReconcileOnce(registry, listFn)

	for _, tr := range trunks {
		if !mgr.IsOwner(tr.ID) {
			t.Fatalf("single-replica cold start: expected ownership of %s after one pass", tr.ID)
		}
	}
}

func TestSingleReplica_RunReconcilerLoop_HandlesContextCancel(t *testing.T) {
	shared := newOwnTestSharedState()
	const replicaID = "r-only"
	if err := shared.SetString(cluster.HeartbeatKeyPrefix+replicaID, "1", 30*time.Second); err != nil {
		t.Fatalf("seed heartbeat: %v", err)
	}
	registry := cluster.NewRegistry(shared)
	if err := registry.Announce(replicaID); err != nil {
		t.Fatalf("announce: %v", err)
	}

	mgr := sip_trunk.NewTrunkOwnershipManager(shared, replicaID)
	trunks := []*sip_trunk.SIPTrunk{{ID: "T1", WorkspaceID: "W1", Enabled: true}}
	listFn := func() ([]*sip_trunk.SIPTrunk, error) {
		out := make([]*sip_trunk.SIPTrunk, len(trunks))
		copy(out, trunks)
		return out, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		mgr.RunReconciler(ctx, registry, listFn)
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for !mgr.IsOwner("T1") {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for immediate first-pass acquisition")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunReconciler did not return after context cancel")
	}
}

func TestSingleReplica_HeartbeatKeyMissing_AbortsReconcile(t *testing.T) {
	shared := newOwnTestSharedState()
	const replicaID = "r-only"

	registry := cluster.NewRegistry(shared)
	if err := registry.Announce(replicaID); err != nil {
		t.Fatalf("announce: %v", err)
	}

	mgr := sip_trunk.NewTrunkOwnershipManager(shared, replicaID)
	trunks := []*sip_trunk.SIPTrunk{{ID: "T1", WorkspaceID: "W1", Enabled: true}}
	mgr.ReconcileOnce(registry, func() ([]*sip_trunk.SIPTrunk, error) {
		return trunks, nil
	})

	if mgr.IsOwner("T1") {
		t.Fatal("must NOT acquire trunk when registry reports zero live replicas")
	}
}

func TestSingleReplica_TrunkBusy_Quiescence(t *testing.T) {
	shared := newOwnTestSharedState()
	const replicaID = "r-only"
	if err := shared.SetString(cluster.HeartbeatKeyPrefix+replicaID, "1", 30*time.Second); err != nil {
		t.Fatalf("seed heartbeat: %v", err)
	}
	registry := cluster.NewRegistry(shared)
	if err := registry.Announce(replicaID); err != nil {
		t.Fatalf("announce: %v", err)
	}

	mgr := sip_trunk.NewTrunkOwnershipManager(shared, replicaID)
	const N = 25
	trunks := make([]*sip_trunk.SIPTrunk, N)
	for i := range trunks {
		trunks[i] = &sip_trunk.SIPTrunk{
			ID:          "T" + strconv.Itoa(i),
			WorkspaceID: "W" + strconv.Itoa(i),
			Enabled:     true,
		}
	}
	mgr.ReconcileOnce(registry, func() ([]*sip_trunk.SIPTrunk, error) {
		out := make([]*sip_trunk.SIPTrunk, len(trunks))
		copy(out, trunks)
		return out, nil
	})

	for _, tr := range trunks {
		if !mgr.IsOwner(tr.ID) {
			t.Fatalf("steady-state single-replica must own %s; would emit ErrTrunkBusy on dial", tr.ID)
		}
	}
}
