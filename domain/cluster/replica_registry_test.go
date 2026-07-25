package cluster

import (
	"sync"
	"testing"
)

type fakeShared struct {
	mu      sync.Mutex
	sets    map[string]map[string]bool
	strings map[string]string
}

func newFakeShared() *fakeShared {
	return &fakeShared{
		sets:    map[string]map[string]bool{},
		strings: map[string]string{},
	}
}

func (f *fakeShared) SAdd(key string, members ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sets[key] == nil {
		f.sets[key] = map[string]bool{}
	}
	for _, m := range members {
		f.sets[key][m] = true
	}
	return nil
}

func (f *fakeShared) SRem(key string, members ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sets[key] == nil {
		return nil
	}
	for _, m := range members {
		delete(f.sets[key], m)
	}
	return nil
}

func (f *fakeShared) SMembers(key string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.sets[key]))
	for m := range f.sets[key] {
		out = append(out, m)
	}
	return out, nil
}

func (f *fakeShared) Exists(key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.strings[key]
	return ok, nil
}

func (f *fakeShared) setHeartbeat(replicaID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.strings[HeartbeatKeyPrefix+replicaID] = "1"
}

func TestRegistry_AnnounceAndLive(t *testing.T) {
	f := newFakeShared()
	r := NewRegistry(f)

	if err := r.Announce("r1"); err != nil {
		t.Fatalf("Announce: %v", err)
	}
	f.setHeartbeat("r1")

	live, err := r.LiveReplicas()
	if err != nil {
		t.Fatalf("LiveReplicas: %v", err)
	}
	if len(live) != 1 || live[0] != "r1" {
		t.Fatalf("expected [r1], got %v", live)
	}
}

func TestRegistry_FiltersStaleMembers(t *testing.T) {
	f := newFakeShared()
	r := NewRegistry(f)

	_ = r.Announce("r1")
	_ = r.Announce("r2")
	_ = r.Announce("r3")
	f.setHeartbeat("r1")
	f.setHeartbeat("r3")

	live, err := r.LiveReplicas()
	if err != nil {
		t.Fatalf("LiveReplicas: %v", err)
	}
	if len(live) != 2 || live[0] != "r1" || live[1] != "r3" {
		t.Fatalf("expected [r1, r3], got %v", live)
	}

	members, _ := f.SMembers(ClusterReplicasKey)
	for _, m := range members {
		if m == "r2" {
			t.Fatalf("expected r2 to be pruned from cluster set, got %v", members)
		}
	}
}

func TestRegistry_Withdraw(t *testing.T) {
	f := newFakeShared()
	r := NewRegistry(f)

	_ = r.Announce("r1")
	_ = r.Announce("r2")
	f.setHeartbeat("r1")
	f.setHeartbeat("r2")

	if err := r.Withdraw("r2"); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}

	live, _ := r.LiveReplicas()
	if len(live) != 1 || live[0] != "r1" {
		t.Fatalf("expected [r1] after withdraw, got %v", live)
	}
}

func TestRegistry_EmptyCluster(t *testing.T) {
	f := newFakeShared()
	r := NewRegistry(f)

	live, err := r.LiveReplicas()
	if err != nil {
		t.Fatalf("LiveReplicas: %v", err)
	}
	if live == nil {
		t.Fatalf("expected empty slice, got nil")
	}
	if len(live) != 0 {
		t.Fatalf("expected empty slice, got %v", live)
	}
}

func TestRegistry_AnnounceEmptyIDIsNoop(t *testing.T) {
	f := newFakeShared()
	r := NewRegistry(f)

	if err := r.Announce(""); err != nil {
		t.Fatalf("Announce empty: %v", err)
	}
	members, _ := f.SMembers(ClusterReplicasKey)
	if len(members) != 0 {
		t.Fatalf("empty replicaID must not be added, got %v", members)
	}
}

func TestRegistry_SortedOutput(t *testing.T) {
	f := newFakeShared()
	r := NewRegistry(f)

	for _, rid := range []string{"r3", "r1", "r2"} {
		_ = r.Announce(rid)
		f.setHeartbeat(rid)
	}

	live, _ := r.LiveReplicas()
	if len(live) != 3 || live[0] != "r1" || live[1] != "r2" || live[2] != "r3" {
		t.Fatalf("expected sorted [r1, r2, r3], got %v", live)
	}
}
