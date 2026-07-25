package voipinfra

import (
	"strconv"
	"sync"
	"testing"

	"vozko/domain/cluster"
	"vozko/domain/sip_trunk"
)

func TestMultiReplicaReconciler_DistributesTrunks(t *testing.T) {
	shared := newOwnTestSharedState()

	const replicaCount = 3
	const trunkCount = 30

	replicaIDs := make([]string, replicaCount)
	for i := range replicaIDs {
		replicaIDs[i] = "replica-" + strconv.Itoa(i)

		shared.forceSet("replica:heartbeat:"+replicaIDs[i], "1")
	}

	for _, rid := range replicaIDs {
		reg := cluster.NewRegistry(shared)
		if err := reg.Announce(rid); err != nil {
			t.Fatalf("announce %s: %v", rid, err)
		}
	}

	registry := cluster.NewRegistry(shared)
	live, err := registry.LiveReplicas()
	if err != nil {
		t.Fatalf("LiveReplicas: %v", err)
	}
	if len(live) != replicaCount {
		t.Fatalf("LiveReplicas() = %d (%v), want %d", len(live), live, replicaCount)
	}

	trunks := make([]*sip_trunk.SIPTrunk, trunkCount)
	for i := range trunks {
		trunks[i] = &sip_trunk.SIPTrunk{
			ID:          "T-" + strconv.Itoa(i),
			WorkspaceID: "W-" + strconv.Itoa(i),
			Enabled:     true,
		}
	}

	listFn := func() ([]*sip_trunk.SIPTrunk, error) {
		out := make([]*sip_trunk.SIPTrunk, len(trunks))
		copy(out, trunks)
		return out, nil
	}

	managers := make([]*sip_trunk.TrunkOwnershipManager, replicaCount)
	for i, rid := range replicaIDs {
		managers[i] = sip_trunk.NewTrunkOwnershipManager(shared, rid)
	}

	var wg sync.WaitGroup
	for _, mgr := range managers {
		wg.Add(1)
		go func(m *sip_trunk.TrunkOwnershipManager) {
			defer wg.Done()
			m.ReconcileOnce(registry, listFn)
		}(mgr)
	}
	wg.Wait()

	owners := map[string]string{}
	for i, mgr := range managers {
		for _, tid := range mgr.OwnedSnapshot() {
			if prev, dup := owners[tid]; dup {
				t.Fatalf("trunk %s owned by both %s and %s", tid, prev, replicaIDs[i])
			}
			owners[tid] = replicaIDs[i]
		}
	}
	if len(owners) != trunkCount {
		t.Fatalf("expected %d trunks owned in total, got %d (owners=%v)", trunkCount, len(owners), owners)
	}

	perReplica := map[string]int{}
	for _, rid := range owners {
		perReplica[rid]++
	}
	for _, rid := range replicaIDs {
		if perReplica[rid] == 0 {
			t.Fatalf("replica %s got zero trunks (perReplica=%v)", rid, perReplica)
		}
	}
}

func TestMultiReplicaReconciler_RebalancesOnReplicaDeath(t *testing.T) {
	shared := newOwnTestSharedState()

	replicaIDs := []string{"r1", "r2", "r3"}
	for _, rid := range replicaIDs {
		shared.forceSet("replica:heartbeat:"+rid, "1")
	}
	for _, rid := range replicaIDs {
		reg := cluster.NewRegistry(shared)
		if err := reg.Announce(rid); err != nil {
			t.Fatalf("announce %s: %v", rid, err)
		}
	}

	trunks := []*sip_trunk.SIPTrunk{}
	for i := 0; i < 12; i++ {
		trunks = append(trunks, &sip_trunk.SIPTrunk{
			ID:          "T" + strconv.Itoa(i),
			WorkspaceID: "W" + strconv.Itoa(i),
			Enabled:     true,
		})
	}
	listFn := func() ([]*sip_trunk.SIPTrunk, error) {
		out := make([]*sip_trunk.SIPTrunk, len(trunks))
		copy(out, trunks)
		return out, nil
	}

	registry := cluster.NewRegistry(shared)
	managers := map[string]*sip_trunk.TrunkOwnershipManager{}
	for _, rid := range replicaIDs {
		managers[rid] = sip_trunk.NewTrunkOwnershipManager(shared, rid)
	}

	for _, mgr := range managers {
		mgr.ReconcileOnce(registry, listFn)
	}

	r2Owned := append([]string(nil), managers["r2"].OwnedSnapshot()...)
	if len(r2Owned) == 0 {
		t.Fatal("setup: r2 owned zero trunks before death; cannot validate rebalance")
	}

	if err := shared.Del("replica:heartbeat:r2"); err != nil {
		t.Fatalf("delete heartbeat: %v", err)
	}

	managers["r1"].ReconcileOnce(registry, listFn)
	managers["r3"].ReconcileOnce(registry, listFn)

	survivors := append([]string(nil), managers["r1"].OwnedSnapshot()...)
	survivors = append(survivors, managers["r3"].OwnedSnapshot()...)
	survivorSet := map[string]struct{}{}
	for _, id := range survivors {
		survivorSet[id] = struct{}{}
	}

	for _, tid := range r2Owned {
		if _, ok := survivorSet[tid]; !ok {
			t.Fatalf("trunk %s previously owned by r2 not picked up after death; survivors=%v", tid, survivors)
		}
	}

	if total := len(survivors); total != len(trunks) {
		t.Fatalf("expected all %d trunks owned by survivors, got %d (%v)", len(trunks), total, survivors)
	}
}
