package whatsapp_campaign_repository

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/shared"
	wc "vozko/domain/whatsapp_campaign"
)

type fakeSharedState struct {
	mu   sync.Mutex
	data map[string]string
}

func newFakeSharedState() *fakeSharedState {
	return &fakeSharedState{data: make(map[string]string)}
}

func (f *fakeSharedState) SetString(key, value string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[key] = value
	return nil
}
func (f *fakeSharedState) GetString(key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.data[key], nil
}
func (f *fakeSharedState) Del(keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		delete(f.data, k)
	}
	return nil
}
func (f *fakeSharedState) Exists(key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.data[key]
	return ok, nil
}

func (f *fakeSharedState) SetNX(string, string, time.Duration) (bool, error) { return true, nil }
func (f *fakeSharedState) Incr(string) (int64, error)                        { return 0, nil }
func (f *fakeSharedState) Decr(string) (int64, error)                        { return 0, nil }
func (f *fakeSharedState) IncrWithTTL(string, time.Duration) (int64, error)  { return 0, nil }
func (f *fakeSharedState) TryIncr(string, int64) (bool, error)               { return true, nil }
func (f *fakeSharedState) SAdd(string, ...string) error                      { return nil }
func (f *fakeSharedState) SRem(string, ...string) error                      { return nil }
func (f *fakeSharedState) SMembers(string) ([]string, error)                 { return nil, nil }
func (f *fakeSharedState) Publish(string, []byte) error                      { return nil }
func (f *fakeSharedState) Subscribe(context.Context, string, func([]byte))   {}
func (f *fakeSharedState) HSet(string, string, string) error                 { return nil }
func (f *fakeSharedState) HDel(string, string) error                         { return nil }
func (f *fakeSharedState) HGetAll(string) (map[string]string, error)         { return nil, nil }
func (f *fakeSharedState) HIncrBy(string, string, int64) (int64, error)      { return 0, nil }
func (f *fakeSharedState) IncrBy(string, int64) (int64, error)               { return 0, nil }
func (f *fakeSharedState) DecrBy(string, int64) (int64, error)               { return 0, nil }
func (f *fakeSharedState) TryIncrBy(string, int64, int64) (bool, error)      { return true, nil }
func (f *fakeSharedState) Expire(string, time.Duration) (bool, error)        { return true, nil }

type fakeRepo struct {
	mu        sync.Mutex
	campaigns map[string]*wc.Campaign
	findCalls int64
	updates   int64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{campaigns: map[string]*wc.Campaign{}}
}

func (f *fakeRepo) put(c *wc.Campaign) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.campaigns[c.ID] = c
}
func (f *fakeRepo) FindByID(id string) (*wc.Campaign, error) {
	atomic.AddInt64(&f.findCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.campaigns[id]
	if !ok {
		return nil, nil
	}
	cp := *c
	return &cp, nil
}
func (f *fakeRepo) Create(c *wc.Campaign) error { f.put(c); return nil }
func (f *fakeRepo) Update(id string, c *wc.Campaign) error {
	atomic.AddInt64(&f.updates, 1)
	f.put(c)
	return nil
}
func (f *fakeRepo) Delete(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.campaigns, id)
	return nil
}
func (f *fakeRepo) UpdateStatus(id string, status wc.Status, _ ...wc.Status) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.campaigns[id]; ok {
		c.Status = status
		return true, nil
	}
	return false, nil
}
func (f *fakeRepo) UpdateResetCode(string, string) error { return nil }
func (f *fakeRepo) UpdateClearCode(string, string) error { return nil }
func (f *fakeRepo) FindLatestOrganicByBusinessPhone(string, string) (*wc.Campaign, error) {
	return nil, nil
}
func (f *fakeRepo) List(wc.ListCampaignsInput) (*shared.PaginatedResult[*wc.Campaign], error) {
	return nil, nil
}
func (f *fakeRepo) ListByStatus(wc.Status) ([]*wc.Campaign, error) {
	return nil, nil
}
func (f *fakeRepo) ListScheduledToStart(time.Time, int) ([]*wc.Campaign, error) {
	return nil, nil
}

func TestCachedRepo_FindByID_CachesAfterFirstRead(t *testing.T) {
	inner := newFakeRepo()
	inner.put(&wc.Campaign{ID: "c1", WorkspaceID: "ws1", TemplateID: "t1"})
	r := NewCachedRepository(inner, newFakeSharedState())

	for i := 0; i < 50; i++ {
		c, err := r.FindByID("c1")
		if err != nil || c == nil {
			t.Fatalf("unexpected: c=%v err=%v", c, err)
		}
	}
	if got := atomic.LoadInt64(&inner.findCalls); got != 1 {
		t.Fatalf("expected 1 inner call, got %d", got)
	}
}

func TestCachedRepo_Update_InvalidatesCache(t *testing.T) {
	inner := newFakeRepo()
	inner.put(&wc.Campaign{ID: "c1", TemplateID: "old"})
	r := NewCachedRepository(inner, newFakeSharedState())

	if _, err := r.FindByID("c1"); err != nil {
		t.Fatal(err)
	}

	updated := &wc.Campaign{ID: "c1", TemplateID: "new"}
	if err := r.Update("c1", updated); err != nil {
		t.Fatal(err)
	}

	got, err := r.FindByID("c1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.TemplateID != "new" {
		t.Fatalf("expected updated template_id, got %#v", got)
	}

	if calls := atomic.LoadInt64(&inner.findCalls); calls != 2 {
		t.Fatalf("expected 2 inner reads, got %d", calls)
	}
}

func TestCachedRepo_UpdateStatus_InvalidatesOnSuccess(t *testing.T) {
	inner := newFakeRepo()
	inner.put(&wc.Campaign{ID: "c1", Status: wc.CampaignStatusRunning})
	r := NewCachedRepository(inner, newFakeSharedState())

	if _, err := r.FindByID("c1"); err != nil {
		t.Fatal(err)
	}

	ok, err := r.UpdateStatus("c1", wc.CampaignStatusStopped, wc.CampaignStatusRunning)
	if err != nil || !ok {
		t.Fatalf("unexpected: ok=%v err=%v", ok, err)
	}

	got, _ := r.FindByID("c1")
	if got == nil || got.Status != wc.CampaignStatusStopped {
		t.Fatalf("expected stopped status after invalidation, got %#v", got)
	}
}

func TestCachedRepo_Delete_InvalidatesCache(t *testing.T) {
	inner := newFakeRepo()
	inner.put(&wc.Campaign{ID: "c1"})
	r := NewCachedRepository(inner, newFakeSharedState())

	if _, err := r.FindByID("c1"); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete("c1"); err != nil {
		t.Fatal(err)
	}

	got, err := r.FindByID("c1")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil after delete+invalidate, got %#v", got)
	}
}

func TestCachedRepo_NilSharedFallsThroughToInner(t *testing.T) {
	inner := newFakeRepo()
	inner.put(&wc.Campaign{ID: "c1"})
	r := NewCachedRepository(inner, nil)

	for i := 0; i < 5; i++ {
		if _, err := r.FindByID("c1"); err != nil {
			t.Fatal(err)
		}
	}
	if calls := atomic.LoadInt64(&inner.findCalls); calls != 5 {
		t.Fatalf("expected 5 inner reads with nil cache, got %d", calls)
	}
}

func TestCachedRepo_FindByID_NotFoundIsNotCached(t *testing.T) {
	inner := newFakeRepo()
	r := NewCachedRepository(inner, newFakeSharedState())

	for i := 0; i < 3; i++ {
		got, err := r.FindByID("missing")
		if err != nil || got != nil {
			t.Fatalf("unexpected: got=%v err=%v", got, err)
		}
	}

	if calls := atomic.LoadInt64(&inner.findCalls); calls != 3 {
		t.Fatalf("expected 3 inner reads for not-found, got %d", calls)
	}
}

func TestCachedRepo_FindByID_EmptyIDPassesThrough(t *testing.T) {
	inner := newFakeRepo()
	r := NewCachedRepository(inner, newFakeSharedState())
	if _, err := r.FindByID(""); err != nil {
		t.Fatal(err)
	}
	if calls := atomic.LoadInt64(&inner.findCalls); calls != 1 {
		t.Fatalf("expected 1 inner read for empty id, got %d", calls)
	}
}

func TestCachedRepo_ConcurrentReadsRaceSafe(t *testing.T) {
	inner := newFakeRepo()
	inner.put(&wc.Campaign{ID: "c1", WorkspaceID: "ws1"})
	r := NewCachedRepository(inner, newFakeSharedState())

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if _, err := r.FindByID("c1"); err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		}()
	}
	wg.Wait()

	if calls := atomic.LoadInt64(&inner.findCalls); calls > int64(goroutines) {
		t.Fatalf("inner called too many times under concurrency: %d", calls)
	}
}

type failingRepo struct{ fakeRepo }

func (f *failingRepo) Update(id string, c *wc.Campaign) error {
	return errors.New("db boom")
}

func TestCachedRepo_UpdateError_DoesNotInvalidate(t *testing.T) {
	inner := &failingRepo{}
	inner.campaigns = map[string]*wc.Campaign{"c1": {ID: "c1", TemplateID: "old"}}

	shared := newFakeSharedState()
	r := NewCachedRepository(inner, shared)

	if _, err := r.FindByID("c1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := shared.data[cacheKey("c1")]; !ok {
		t.Fatal("expected cache to be populated")
	}

	if err := r.Update("c1", &wc.Campaign{ID: "c1", TemplateID: "new"}); err == nil {
		t.Fatal("expected update error to propagate")
	}

	if _, ok := shared.data[cacheKey("c1")]; !ok {
		t.Fatal("expected cache to remain after failed update")
	}
}
