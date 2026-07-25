package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/agent"
	"vozko/domain/shared"
)

type fakeShared struct {
	mu   sync.Mutex
	data map[string]string
}

func newFakeShared() *fakeShared { return &fakeShared{data: map[string]string{}} }

func (f *fakeShared) SetString(k, v string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[k] = v
	return nil
}
func (f *fakeShared) GetString(k string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.data[k], nil
}
func (f *fakeShared) Del(keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		delete(f.data, k)
	}
	return nil
}
func (f *fakeShared) Exists(k string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.data[k]
	return ok, nil
}

func (f *fakeShared) SetNX(string, string, time.Duration) (bool, error) { return true, nil }
func (f *fakeShared) Incr(string) (int64, error)                        { return 0, nil }
func (f *fakeShared) Decr(string) (int64, error)                        { return 0, nil }
func (f *fakeShared) IncrWithTTL(string, time.Duration) (int64, error)  { return 0, nil }
func (f *fakeShared) TryIncr(string, int64) (bool, error)               { return true, nil }
func (f *fakeShared) SAdd(string, ...string) error                      { return nil }
func (f *fakeShared) SRem(string, ...string) error                      { return nil }
func (f *fakeShared) SMembers(string) ([]string, error)                 { return nil, nil }
func (f *fakeShared) Publish(string, []byte) error                      { return nil }
func (f *fakeShared) Subscribe(context.Context, string, func([]byte))   {}
func (f *fakeShared) HSet(string, string, string) error                 { return nil }
func (f *fakeShared) HDel(string, string) error                         { return nil }
func (f *fakeShared) HGetAll(string) (map[string]string, error)         { return nil, nil }
func (f *fakeShared) HIncrBy(string, string, int64) (int64, error)      { return 0, nil }
func (f *fakeShared) IncrBy(string, int64) (int64, error)               { return 0, nil }
func (f *fakeShared) DecrBy(string, int64) (int64, error)               { return 0, nil }
func (f *fakeShared) TryIncrBy(string, int64, int64) (bool, error)      { return true, nil }
func (f *fakeShared) Expire(string, time.Duration) (bool, error)        { return true, nil }

type fakeAgentRepo struct {
	mu        sync.Mutex
	agents    map[string]*agent.Agent
	findCalls int64
}

func newFakeAgentRepo() *fakeAgentRepo {
	return &fakeAgentRepo{agents: map[string]*agent.Agent{}}
}

func (f *fakeAgentRepo) put(a *agent.Agent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *a
	f.agents[a.ID] = &cp
}

func (f *fakeAgentRepo) FindByIDs([]string) ([]*agent.Agent, error) { return nil, nil }

func (f *fakeAgentRepo) FindByID(id string) (*agent.Agent, error) {
	atomic.AddInt64(&f.findCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.agents[id]
	if !ok {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (f *fakeAgentRepo) Create(a *agent.Agent) error            { f.put(a); return nil }
func (f *fakeAgentRepo) Update(id string, a *agent.Agent) error { f.put(a); return nil }
func (f *fakeAgentRepo) Delete(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.agents, id)
	return nil
}
func (f *fakeAgentRepo) List(agent.ListAgentsInput) (*shared.PaginatedResult[*agent.AgentListItem], error) {
	return nil, nil
}

func newAgent(id string) *agent.Agent {
	return &agent.Agent{ID: id, MessagingPrompt: "be helpful"}
}

func TestCachedAgent_FindByID_CachesAfterFirstRead(t *testing.T) {
	t.Parallel()
	inner := newFakeAgentRepo()
	inner.put(newAgent("a-1"))
	cached := NewCachedRepository(inner, newFakeShared())

	for i := 0; i < 5; i++ {
		got, err := cached.FindByID("a-1")
		if err != nil || got == nil || got.ID != "a-1" {
			t.Fatalf("iter %d: %+v err=%v", i, got, err)
		}
	}
	if c := atomic.LoadInt64(&inner.findCalls); c != 1 {
		t.Fatalf("expected 1 inner FindByID, got %d", c)
	}
}

func TestCachedAgent_Update_InvalidatesCache(t *testing.T) {
	t.Parallel()
	inner := newFakeAgentRepo()
	inner.put(newAgent("a-1"))
	cached := NewCachedRepository(inner, newFakeShared())

	_, _ = cached.FindByID("a-1")

	updated := newAgent("a-1")
	updated.MessagingPrompt = "new prompt"
	if err := cached.Update("a-1", updated); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := cached.FindByID("a-1")
	if err != nil || got == nil || got.MessagingPrompt != "new prompt" {
		t.Fatalf("post-update: %+v err=%v", got, err)
	}
}

func TestCachedAgent_Delete_InvalidatesCache(t *testing.T) {
	t.Parallel()
	inner := newFakeAgentRepo()
	inner.put(newAgent("a-1"))
	cached := NewCachedRepository(inner, newFakeShared())

	_, _ = cached.FindByID("a-1")
	if err := cached.Delete("a-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ := cached.FindByID("a-1")
	if got != nil {
		t.Fatalf("expected nil after delete, got %+v", got)
	}
}

func TestCachedAgent_Create_InvalidatesPrePopulatedKey(t *testing.T) {
	t.Parallel()
	inner := newFakeAgentRepo()
	cached := NewCachedRepository(inner, newFakeShared())

	if got, _ := cached.FindByID("a-2"); got != nil {
		t.Fatalf("preflight: %+v", got)
	}
	if err := cached.Create(newAgent("a-2")); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := cached.FindByID("a-2")
	if err != nil || got == nil || got.ID != "a-2" {
		t.Fatalf("post-create: %+v err=%v", got, err)
	}
}

func TestCachedAgent_NotFound_IsNotCached(t *testing.T) {
	t.Parallel()
	inner := newFakeAgentRepo()
	cached := NewCachedRepository(inner, newFakeShared())

	for i := 0; i < 3; i++ {
		if got, _ := cached.FindByID("missing"); got != nil {
			t.Fatalf("iter %d unexpected hit %+v", i, got)
		}
	}
	if c := atomic.LoadInt64(&inner.findCalls); c != 3 {
		t.Fatalf("expected 3 inner calls, got %d", c)
	}
}

func TestCachedAgent_NilSharedFallsThrough(t *testing.T) {
	t.Parallel()
	inner := newFakeAgentRepo()
	inner.put(newAgent("a-1"))
	cached := NewCachedRepository(inner, nil)

	for i := 0; i < 4; i++ {
		got, err := cached.FindByID("a-1")
		if err != nil || got == nil {
			t.Fatalf("iter %d: %+v err=%v", i, got, err)
		}
	}
	if c := atomic.LoadInt64(&inner.findCalls); c != 4 {
		t.Fatalf("without cache expected 4 inner calls, got %d", c)
	}
}

func TestCachedAgent_EmptyIDPassesThrough(t *testing.T) {
	t.Parallel()
	inner := newFakeAgentRepo()
	cached := NewCachedRepository(inner, newFakeShared())
	if _, err := cached.FindByID(""); err != nil {
		t.Fatalf("empty id: %v", err)
	}
}

func TestCachedAgent_ConcurrentReadsRaceSafe(t *testing.T) {
	t.Parallel()
	inner := newFakeAgentRepo()
	inner.put(newAgent("a-1"))
	cached := NewCachedRepository(inner, newFakeShared())

	const N = 32
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, _ = cached.FindByID("a-1")
			}
		}()
	}
	wg.Wait()
}
