package apikey_usecase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/apikey"
)

type fakeKeys struct {
	mu      sync.Mutex
	keys    map[string]*apikey.Key
	lookups int
	touches int
	fail    error
}

func newFakeKeys() *fakeKeys { return &fakeKeys{keys: map[string]*apikey.Key{}} }

func (f *fakeKeys) Create(_ context.Context, k *apikey.Key) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := *k
	f.keys[k.ID] = &c
	return nil
}

func (f *fakeKeys) ListByWorkspace(_ context.Context, ws string) ([]apikey.Key, error) {
	var out []apikey.Key
	for _, k := range f.keys {
		if k.WorkspaceID == ws {
			out = append(out, *k)
		}
	}
	return out, nil
}

func (f *fakeKeys) GetByLookupID(_ context.Context, lookup string) (*apikey.Key, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups++
	if f.fail != nil {
		return nil, f.fail
	}
	for _, k := range f.keys {
		if k.LookupID == lookup {
			c := *k
			return &c, nil
		}
	}
	return nil, apikey.ErrNotFound
}

func (f *fakeKeys) Get(_ context.Context, ws, id string) (*apikey.Key, error) {
	k, ok := f.keys[id]
	if !ok || k.WorkspaceID != ws {
		return nil, apikey.ErrNotFound
	}
	c := *k
	return &c, nil
}

func (f *fakeKeys) Revoke(ctx context.Context, ws, id, by string, at time.Time) (*apikey.Key, error) {
	k, ok := f.keys[id]
	if !ok || k.WorkspaceID != ws {
		return nil, apikey.ErrNotFound
	}
	if k.RevokedAt == nil {
		k.RevokedAt, k.RevokedBy = &at, by
	}
	return f.Get(ctx, ws, id)
}

func (f *fakeKeys) Touch(context.Context, string, time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touches++
	return nil
}

func (f *fakeKeys) CountActive(context.Context, string) (int64, error) { return 0, nil }

type fakeCache struct {
	mu      sync.Mutex
	values  map[string]string
	failGet error
	failSet error
	failDel error
}

func newFakeCache() *fakeCache { return &fakeCache{values: map[string]string{}} }

func (c *fakeCache) GetString(key string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failGet != nil {
		return "", c.failGet
	}
	return c.values[key], nil
}

func (c *fakeCache) SetString(key, value string, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failSet != nil {
		return c.failSet
	}
	c.values[key] = value
	return nil
}

func (c *fakeCache) SetNX(key, value string, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.values[key]; ok {
		return false, nil
	}
	c.values[key] = value
	return true, nil
}

func (c *fakeCache) Del(keys ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failDel != nil {
		return c.failDel
	}
	for _, k := range keys {
		delete(c.values, k)
	}
	return nil
}

func newService(t *testing.T) (*Service, *fakeKeys, *fakeCache) {
	t.Helper()
	keys, cache := newFakeKeys(), newFakeCache()
	svc, err := New(Deps{Keys: keys, Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	return svc, keys, cache
}

func created(t *testing.T, svc *Service, scopes ...string) Created {
	t.Helper()
	c, err := svc.Create(context.Background(), CreateInput{WorkspaceID: "ws-1", UserID: "u-1", Name: "App", Scopes: scopes})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCreateReturnsThePlainKeyOnceAndStoresOnlyItsHash(t *testing.T) {
	svc, keys, _ := newService(t)
	c := created(t, svc, "email:send")
	stored := keys.keys[c.Key.ID]
	if !strings.HasPrefix(c.Plain, "wsk_") || stored.SecretHash == c.Plain || !apikey.Matches(stored.SecretHash, c.Plain) || stored.CreatedBy != "u-1" {
		t.Fatalf("stored = %+v", stored)
	}
	if _, err := svc.Create(context.Background(), CreateInput{WorkspaceID: "ws-1", Name: "", Scopes: []string{"email:send"}}); !errors.Is(err, apikey.ErrNameInvalid) {
		t.Fatalf("no name = %v", err)
	}
	if _, err := svc.Create(context.Background(), CreateInput{WorkspaceID: "ws-1", Name: "x", Scopes: []string{"admin:all"}}); !errors.Is(err, apikey.ErrScopesInvalid) {
		t.Fatalf("unknown scope = %v", err)
	}
}

func TestAuthenticateCachesTheVerifiedKeyAndTouchesOncePerInterval(t *testing.T) {
	svc, keys, _ := newService(t)
	c := created(t, svc, "email:send")
	for i := 0; i < 5; i++ {
		p, err := svc.Authenticate(context.Background(), c.Plain, apikey.ScopeEmailSend)
		if err != nil || p.WorkspaceID != "ws-1" || p.KeyID != c.Key.ID {
			t.Fatalf("principal = %+v %v", p, err)
		}
	}
	if keys.lookups != 1 || keys.touches != 1 {
		t.Fatalf("lookups %d touches %d, want one of each for five calls", keys.lookups, keys.touches)
	}
}

func TestAuthenticateFailsClosed(t *testing.T) {
	svc, keys, cache := newService(t)
	c := created(t, svc, "email:read")
	cases := []struct {
		name  string
		token string
		scope apikey.Scope
		setup func()
		want  error
	}{
		{"missing", "", apikey.ScopeEmailRead, func() {}, ErrUnauthenticated},
		{"malformed", "Bearer abc", apikey.ScopeEmailRead, func() {}, ErrInvalid},
		{"unknown", "wsk_aaaaaaaaaaaa_" + strings.Repeat("b", 43), apikey.ScopeEmailRead, func() {}, ErrInvalid},
		{"wrong secret", c.Plain[:len(c.Plain)-1] + "Z", apikey.ScopeEmailRead, func() {}, ErrInvalid},
		{"scope", c.Plain, apikey.ScopeEmailSend, func() {}, ErrScopeMissing},
		{"redis down", c.Plain, apikey.ScopeEmailRead, func() { cache.failGet = errors.New("down") }, ErrUnavailable},
		{"database down", c.Plain, apikey.ScopeEmailRead, func() { cache.failGet = nil; cache.values = map[string]string{}; keys.fail = errors.New("down") }, ErrUnavailable},
		{"cache write down", c.Plain, apikey.ScopeEmailRead, func() { keys.fail = nil; cache.failSet = errors.New("down") }, ErrUnavailable},
	}
	for _, tc := range cases {
		tc.setup()
		if _, err := svc.Authenticate(context.Background(), tc.token, tc.scope); !errors.Is(err, tc.want) {
			t.Fatalf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestRevokeTakesEffectAtOnceEvenWithAWarmCache(t *testing.T) {
	svc, _, cache := newService(t)
	c := created(t, svc, "email:send")
	if _, err := svc.Authenticate(context.Background(), c.Plain, apikey.ScopeEmailSend); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Revoke(context.Background(), "ws-1", c.Key.ID, "u-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(context.Background(), c.Plain, apikey.ScopeEmailSend); !errors.Is(err, apikey.ErrRevoked) {
		t.Fatalf("after revoke = %v", err)
	}
	if cache.values[cachePrefix+c.Key.LookupID] != "" {
		t.Fatal("a revoked key stayed in the cache")
	}
}

func TestRevokeReportsACacheThatCouldNotBeCleared(t *testing.T) {
	svc, keys, cache := newService(t)
	c := created(t, svc, "email:send")
	cache.failDel = errors.New("down")
	if _, err := svc.Revoke(context.Background(), "ws-1", c.Key.ID, "u-2"); !errors.Is(err, ErrCacheUnavailable) {
		t.Fatalf("revoke = %v", err)
	}
	if keys.keys[c.Key.ID].RevokedAt == nil {
		t.Fatal("the key must be revoked in the database even when the cache fails")
	}
	cache.failDel = nil
	if _, err := svc.Revoke(context.Background(), "ws-1", c.Key.ID, "u-2"); err != nil {
		t.Fatalf("a repeated revoke clears the cache: %v", err)
	}
}

func TestAKeyCachedInARaceWithRevokeIsRefused(t *testing.T) {
	svc, _, cache := newService(t)
	c := created(t, svc, "email:send")
	cache.values[tombstonePrefix+c.Key.LookupID] = "1"
	if _, err := svc.Authenticate(context.Background(), c.Plain, apikey.ScopeEmailSend); !errors.Is(err, apikey.ErrRevoked) {
		t.Fatalf("auth with a tombstone = %v", err)
	}
	if cache.values[cachePrefix+c.Key.LookupID] != "" {
		t.Fatal("the racing entry must be removed")
	}
}
