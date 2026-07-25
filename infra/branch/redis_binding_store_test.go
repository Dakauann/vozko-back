package branchinfra

import (
	"testing"
	"time"

	domainCache "vozko/domain/cache"
)

// fakeSharedState is a minimal in-memory stand-in for the Redis SharedState: it
// implements only the string+set ops the binding store uses (TTL is a no-op because
// the store enforces logical expiry via ExpiresAt, not the Redis backstop). Any other
// SharedState method panics, which is intended.
type fakeSharedState struct {
	domainCache.SharedState
	strs map[string]string
	sets map[string]map[string]struct{}
}

func newFakeSharedState() *fakeSharedState {
	return &fakeSharedState{strs: map[string]string{}, sets: map[string]map[string]struct{}{}}
}

func (f *fakeSharedState) SetString(key, value string, _ time.Duration) error {
	f.strs[key] = value
	return nil
}
func (f *fakeSharedState) GetString(key string) (string, error) { return f.strs[key], nil }
func (f *fakeSharedState) Del(keys ...string) error {
	for _, k := range keys {
		delete(f.strs, k)
		delete(f.sets, k)
	}
	return nil
}
func (f *fakeSharedState) SAdd(key string, members ...string) error {
	if f.sets[key] == nil {
		f.sets[key] = map[string]struct{}{}
	}
	for _, m := range members {
		f.sets[key][m] = struct{}{}
	}
	return nil
}
func (f *fakeSharedState) SRem(key string, members ...string) error {
	if f.sets[key] != nil {
		for _, m := range members {
			delete(f.sets[key], m)
		}
	}
	return nil
}
func (f *fakeSharedState) SMembers(key string) ([]string, error) {
	out := make([]string, 0, len(f.sets[key]))
	for m := range f.sets[key] {
		out = append(out, m)
	}
	return out, nil
}
func (f *fakeSharedState) Expire(string, time.Duration) (bool, error) { return true, nil }

func rbNow() time.Time { return time.Unix(1_700_000_000, 0) }

func TestRedisBindingStore_UpsertListRemove(t *testing.T) {
	store := NewRedisBindingStore(newFakeSharedState(), rbNow)

	b := bind("1001", "call-a", rbNow().Add(time.Minute))
	b.ReceivedFrom = "203.0.113.7:41000"
	b.BranchID = "branch-1001"
	if err := store.Upsert(b); err != nil {
		t.Fatal(err)
	}

	live, err := store.ListLive("1001")
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || live[0].ReceivedFrom != "203.0.113.7:41000" || live[0].BranchID != "branch-1001" {
		t.Fatalf("ListLive = %+v, want one binding with rewrite_contact + branch preserved", live)
	}
	if n, _ := store.CountLive("1001"); n != 1 {
		t.Fatalf("CountLive = %d, want 1", n)
	}

	if err := store.Remove("1001", "call-a"); err != nil {
		t.Fatal(err)
	}
	if n, _ := store.CountLive("1001"); n != 0 {
		t.Fatalf("CountLive after remove = %d, want 0", n)
	}
}

func TestRedisBindingStore_MultiContact(t *testing.T) {
	store := NewRedisBindingStore(newFakeSharedState(), rbNow)
	_ = store.Upsert(bind("1001", "call-a", rbNow().Add(time.Minute)))
	_ = store.Upsert(bind("1001", "call-b", rbNow().Add(time.Minute)))

	if n, _ := store.CountLive("1001"); n != 2 {
		t.Fatalf("CountLive = %d, want 2", n)
	}
}

func TestRedisBindingStore_ExpiryFiltering(t *testing.T) {
	store := NewRedisBindingStore(newFakeSharedState(), rbNow)
	_ = store.Upsert(bind("1001", "live", rbNow().Add(time.Minute)))
	_ = store.Upsert(bind("1001", "dead", rbNow().Add(-time.Minute))) // already expired

	live, _ := store.ListLive("1001")
	if len(live) != 1 || live[0].CallID != "live" {
		t.Fatalf("ListLive = %+v, want only the unexpired contact", live)
	}
}

func TestRedisBindingStore_ReapReturnsEvicted(t *testing.T) {
	store := NewRedisBindingStore(newFakeSharedState(), rbNow)
	_ = store.Upsert(bind("1001", "live", rbNow().Add(time.Minute)))
	exp := bind("1001", "dead", rbNow().Add(-time.Second))
	exp.BranchID = "branch-1001"
	_ = store.Upsert(exp)

	evicted := store.ReapExpired(rbNow())
	if len(evicted) != 1 || evicted[0].CallID != "dead" || evicted[0].BranchID != "branch-1001" {
		t.Fatalf("ReapExpired = %+v, want the expired binding returned (drives presence-offline)", evicted)
	}
	// The live contact survives.
	if n, _ := store.CountLive("1001"); n != 1 {
		t.Fatalf("CountLive after reap = %d, want 1", n)
	}
}

func TestRedisBindingStore_ListAllLiveForRehydration(t *testing.T) {
	store := NewRedisBindingStore(newFakeSharedState(), rbNow)
	_ = store.Upsert(bind("1001", "a", rbNow().Add(time.Minute)))
	_ = store.Upsert(bind("1002", "b", rbNow().Add(time.Minute)))

	rehydrator, ok := store.(BindingRehydrator)
	if !ok {
		t.Fatal("redis store must implement BindingRehydrator")
	}
	all, err := rehydrator.ListAllLive()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("ListAllLive = %d bindings, want 2 (one per AOR)", len(all))
	}
}
