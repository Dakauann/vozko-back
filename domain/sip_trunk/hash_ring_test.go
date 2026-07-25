package sip_trunk

import (
	"fmt"
	"testing"
)

func TestAssignOwner_Empty(t *testing.T) {
	if got := AssignOwner("ws-1", nil); got != "" {
		t.Fatalf("expected empty string for nil replicas, got %q", got)
	}
	if got := AssignOwner("ws-1", []string{}); got != "" {
		t.Fatalf("expected empty string for empty replicas, got %q", got)
	}
}

func TestAssignOwner_SingleReplica(t *testing.T) {
	for _, key := range []string{"a", "b", "ws-1", "ws-2", ""} {
		if got := AssignOwner(key, []string{"r1"}); got != "r1" {
			t.Fatalf("single replica must own everything, got %q for key %q", got, key)
		}
	}
}

func TestAssignOwner_Deterministic(t *testing.T) {
	replicas := []string{"r1", "r2", "r3"}
	for _, key := range []string{"ws-A", "ws-B", "ws-C", "ws-X", "ws-Y"} {
		first := AssignOwner(key, replicas)
		for i := 0; i < 1000; i++ {
			if got := AssignOwner(key, replicas); got != first {
				t.Fatalf("non-deterministic: key=%s first=%s got=%s", key, first, got)
			}
		}
	}
}

func TestAssignOwner_OrderIndependent(t *testing.T) {

	keys := []string{"ws-1", "ws-2", "ws-3", "ws-4", "ws-5"}
	a := AssignAll(keys, []string{"r1", "r2", "r3"})
	b := AssignAll(keys, []string{"r3", "r1", "r2"})
	c := AssignAll(keys, []string{"r2", "r3", "r1"})
	for _, k := range keys {
		if a[k] != b[k] || b[k] != c[k] {
			t.Fatalf("order-dependent: key=%s a=%s b=%s c=%s", k, a[k], b[k], c[k])
		}
	}
}

func TestAssignOwner_IgnoresEmptyReplicaIDs(t *testing.T) {
	withEmpty := AssignOwner("ws-1", []string{"", "r1", "", "r2"})
	clean := AssignOwner("ws-1", []string{"r1", "r2"})
	if withEmpty != clean {
		t.Fatalf("empty replica IDs must be ignored: withEmpty=%q clean=%q", withEmpty, clean)
	}
}

func TestAssignAll_Distribution(t *testing.T) {

	keys := make([]string, 1000)
	for i := range keys {
		keys[i] = fmt.Sprintf("ws-%d", i)
	}
	replicas := []string{"r1", "r2", "r3"}
	out := AssignAll(keys, replicas)

	counts := map[string]int{}
	for _, v := range out {
		counts[v]++
	}
	for _, r := range replicas {
		if counts[r] < 250 || counts[r] > 420 {
			t.Fatalf("imbalance: replica %s got %d (expected ~333)", r, counts[r])
		}
	}
	if counts["r1"]+counts["r2"]+counts["r3"] != 1000 {
		t.Fatalf("missing assignments: counts=%v", counts)
	}
}

func TestAssignAll_StableOnReplicaRemoval(t *testing.T) {

	keys := make([]string, 1000)
	for i := range keys {
		keys[i] = fmt.Sprintf("ws-%d", i)
	}
	before := AssignAll(keys, []string{"r1", "r2", "r3", "r4"})
	after := AssignAll(keys, []string{"r1", "r2", "r3"})

	moved := 0
	for k, v := range before {
		if after[k] != v {
			moved++
		}
	}

	formerlyR4 := 0
	for _, v := range before {
		if v == "r4" {
			formerlyR4++
		}
	}
	if moved != formerlyR4 {
		t.Fatalf("only r4's keys must move: moved=%d formerlyR4=%d", moved, formerlyR4)
	}
}

func TestAssignAll_StableOnReplicaAddition(t *testing.T) {

	keys := make([]string, 1000)
	for i := range keys {
		keys[i] = fmt.Sprintf("ws-%d", i)
	}
	before := AssignAll(keys, []string{"r1", "r2", "r3"})
	after := AssignAll(keys, []string{"r1", "r2", "r3", "r4"})

	moved := 0
	movedTo := map[string]int{}
	for k, v := range before {
		if after[k] != v {
			moved++
			movedTo[after[k]]++
		}
	}

	if movedTo["r4"] != moved {
		t.Fatalf("only keys won by r4 should move: moved=%d to_r4=%d total_movedTo=%v", moved, movedTo["r4"], movedTo)
	}

	if moved < 150 || moved > 350 {
		t.Fatalf("unexpected move count: moved=%d (expected ~250)", moved)
	}
}

func TestAssignAll_WorkspacesGroupedToSameReplica(t *testing.T) {

	keys := []string{"ws-A", "ws-B", "ws-A", "ws-C", "ws-B", "ws-A"}
	out := AssignAll(keys, []string{"r1", "r2", "r3"})
	if len(out) != 3 {
		t.Fatalf("expected 3 unique keys mapped, got %d", len(out))
	}
}
