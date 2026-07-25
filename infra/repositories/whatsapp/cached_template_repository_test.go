package whatsapp_repository

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/shared"
	"vozko/domain/whatsapp/template"
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

type fakeTemplateRepo struct {
	mu        sync.Mutex
	templates map[string]*template.Template
	findCalls int64
}

func newFakeTemplateRepo() *fakeTemplateRepo {
	return &fakeTemplateRepo{templates: map[string]*template.Template{}}
}

func (f *fakeTemplateRepo) put(t *template.Template) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.templates[t.ID] = t
}

func (f *fakeTemplateRepo) FindByID(id string) (*template.Template, error) {
	atomic.AddInt64(&f.findCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.templates[id]
	if !ok {
		return nil, nil
	}
	cp := *t
	return &cp, nil
}

func (f *fakeTemplateRepo) Create(t *template.Template) error { f.put(t); return nil }
func (f *fakeTemplateRepo) Update(id string, t *template.Template) error {
	f.put(t)
	return nil
}
func (f *fakeTemplateRepo) Delete(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.templates, id)
	return nil
}
func (f *fakeTemplateRepo) UpdateStatus(id string, s template.TemplateStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.templates[id]; ok {
		t.Status = s
	}
	return nil
}
func (f *fakeTemplateRepo) UpdateHeaderMediaURL(string, *string) error {
	return nil
}
func (f *fakeTemplateRepo) UpdateHeaderMedia(string, *string, *string) error {
	return nil
}
func (f *fakeTemplateRepo) SyncFromExternal(t *template.Template) error {
	f.put(t)
	return nil
}

func (f *fakeTemplateRepo) FindByExternalID(string) (*template.Template, error) { return nil, nil }
func (f *fakeTemplateRepo) FindByExternalIDAndWABA(string, string) (*template.Template, error) {
	return nil, nil
}
func (f *fakeTemplateRepo) BatchFindByExternalIDs([]string) ([]*template.Template, error) {
	return nil, nil
}
func (f *fakeTemplateRepo) FindByName(string, string) (*template.Template, error) {
	return nil, nil
}
func (f *fakeTemplateRepo) FindByNameAndWABA(string, string, string) (*template.Template, error) {
	return nil, nil
}
func (f *fakeTemplateRepo) List(template.ListInput) (*shared.PaginatedResult[*template.Template], error) {
	return nil, nil
}

func TestCachedTemplate_FindByID_CachesAfterFirstRead(t *testing.T) {
	inner := newFakeTemplateRepo()
	inner.put(&template.Template{ID: "t1", Name: "hello", Status: template.TemplateStatusApproved})
	r := NewCachedTemplateRepository(inner, newFakeSharedState())

	for i := 0; i < 50; i++ {
		got, err := r.FindByID("t1")
		if err != nil || got == nil {
			t.Fatalf("unexpected: got=%v err=%v", got, err)
		}
	}
	if calls := atomic.LoadInt64(&inner.findCalls); calls != 1 {
		t.Fatalf("expected 1 inner call, got %d", calls)
	}
}

func TestCachedTemplate_UpdateStatus_InvalidatesCache(t *testing.T) {
	inner := newFakeTemplateRepo()
	inner.put(&template.Template{ID: "t1", Status: template.TemplateStatusApproved})
	r := NewCachedTemplateRepository(inner, newFakeSharedState())

	if _, err := r.FindByID("t1"); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateStatus("t1", template.TemplateStatusRejected); err != nil {
		t.Fatal(err)
	}
	got, err := r.FindByID("t1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Status != template.TemplateStatusRejected {
		t.Fatalf("expected REJECTED status after invalidation, got %#v", got)
	}
}

func TestCachedTemplate_Update_InvalidatesCache(t *testing.T) {
	inner := newFakeTemplateRepo()
	inner.put(&template.Template{ID: "t1", Name: "old"})
	r := NewCachedTemplateRepository(inner, newFakeSharedState())

	if _, err := r.FindByID("t1"); err != nil {
		t.Fatal(err)
	}
	if err := r.Update("t1", &template.Template{ID: "t1", Name: "new"}); err != nil {
		t.Fatal(err)
	}
	got, _ := r.FindByID("t1")
	if got == nil || got.Name != "new" {
		t.Fatalf("expected updated name, got %#v", got)
	}
}

func TestCachedTemplate_Delete_InvalidatesCache(t *testing.T) {
	inner := newFakeTemplateRepo()
	inner.put(&template.Template{ID: "t1"})
	r := NewCachedTemplateRepository(inner, newFakeSharedState())

	if _, err := r.FindByID("t1"); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete("t1"); err != nil {
		t.Fatal(err)
	}
	got, err := r.FindByID("t1")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil after delete+invalidate, got %#v", got)
	}
}

func TestCachedTemplate_SyncFromExternal_InvalidatesCache(t *testing.T) {
	inner := newFakeTemplateRepo()
	inner.put(&template.Template{ID: "t1", Status: template.TemplateStatusPending})
	r := NewCachedTemplateRepository(inner, newFakeSharedState())

	if _, err := r.FindByID("t1"); err != nil {
		t.Fatal(err)
	}
	if err := r.SyncFromExternal(&template.Template{ID: "t1", Status: template.TemplateStatusApproved}); err != nil {
		t.Fatal(err)
	}
	got, _ := r.FindByID("t1")
	if got == nil || got.Status != template.TemplateStatusApproved {
		t.Fatalf("expected APPROVED status after sync invalidation, got %#v", got)
	}
}

func TestCachedTemplate_NilSharedFallsThrough(t *testing.T) {
	inner := newFakeTemplateRepo()
	inner.put(&template.Template{ID: "t1"})
	r := NewCachedTemplateRepository(inner, nil)

	for i := 0; i < 5; i++ {
		if _, err := r.FindByID("t1"); err != nil {
			t.Fatal(err)
		}
	}
	if calls := atomic.LoadInt64(&inner.findCalls); calls != 5 {
		t.Fatalf("expected 5 inner reads with nil cache, got %d", calls)
	}
}

func TestCachedTemplate_NotFoundIsNotCached(t *testing.T) {
	inner := newFakeTemplateRepo()
	r := NewCachedTemplateRepository(inner, newFakeSharedState())

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

func TestCachedTemplate_ConcurrentReadsRaceSafe(t *testing.T) {
	inner := newFakeTemplateRepo()
	inner.put(&template.Template{ID: "t1"})
	r := NewCachedTemplateRepository(inner, newFakeSharedState())

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if _, err := r.FindByID("t1"); err != nil {
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
