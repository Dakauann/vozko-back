package lead

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeState struct {
	mu     sync.Mutex
	values map[string]string
	fail   bool
	gets   int
	sets   int
}

func newFakeState() *fakeState {
	return &fakeState{values: map[string]string{}}
}

func (f *fakeState) GetString(key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	if f.fail {
		return "", errors.New("cache down")
	}
	return f.values[key], nil
}

func (f *fakeState) SetString(key, value string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets++
	if f.fail {
		return errors.New("cache down")
	}
	f.values[key] = value
	return nil
}

func (f *fakeState) Incr(key string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return 0, errors.New("cache down")
	}
	var n int64
	if existing, ok := f.values[key]; ok {
		for _, r := range existing {
			n = n*10 + int64(r-'0')
		}
	}
	n++
	f.values[key] = itoa(n)
	return n, nil
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func (f *fakeState) Expire(key string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.values[key]
	return ok, nil
}

func (f *fakeState) Del(keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		delete(f.values, k)
	}
	return nil
}

func (f *fakeState) SetNX(string, string, time.Duration) (bool, error) { panic("unused") }
func (f *fakeState) Exists(string) (bool, error)                       { panic("unused") }
func (f *fakeState) Decr(string) (int64, error)                        { panic("unused") }
func (f *fakeState) IncrWithTTL(string, time.Duration) (int64, error)  { panic("unused") }
func (f *fakeState) TryIncr(string, int64) (bool, error)               { panic("unused") }
func (f *fakeState) SAdd(string, ...string) error                      { panic("unused") }
func (f *fakeState) SRem(string, ...string) error                      { panic("unused") }
func (f *fakeState) SMembers(string) ([]string, error)                 { panic("unused") }
func (f *fakeState) Publish(string, []byte) error                      { panic("unused") }
func (f *fakeState) Subscribe(context.Context, string, func([]byte))   { panic("unused") }
func (f *fakeState) HSet(string, string, string) error                 { panic("unused") }
func (f *fakeState) HDel(string, string) error                         { panic("unused") }
func (f *fakeState) HGetAll(string) (map[string]string, error)         { panic("unused") }
func (f *fakeState) HIncrBy(string, string, int64) (int64, error)      { panic("unused") }
func (f *fakeState) IncrBy(string, int64) (int64, error)               { panic("unused") }
func (f *fakeState) DecrBy(string, int64) (int64, error)               { panic("unused") }
func (f *fakeState) TryIncrBy(string, int64, int64) (bool, error)      { panic("unused") }

func testQuery(where string, args ...interface{}) *listQuery {
	return &listQuery{where: where, args: args}
}

func TestAggregateKeyVariesWithTheFilter(t *testing.T) {
	c := newAggregateCache(newFakeState())

	base := c.key("count", "ws-1", testQuery("leads.blocked = ?", true))
	other := c.key("count", "ws-1", testQuery("leads.blocked = ?", false))
	if base == other {
		t.Fatal("filters differing only in a bound argument share a cache key")
	}

	different := c.key("count", "ws-1", testQuery("leads.name ILIKE ?", true))
	if base == different {
		t.Fatal("filters differing in SQL share a cache key")
	}
}

func TestAggregateKeyIsStable(t *testing.T) {
	c := newAggregateCache(newFakeState())
	q := testQuery("leads.blocked = ?", true)

	if c.key("count", "ws-1", q) != c.key("count", "ws-1", q) {
		t.Fatal("the same query produced two different keys")
	}
}

func TestAggregateKeyIsScopedPerWorkspaceAndKind(t *testing.T) {
	c := newAggregateCache(newFakeState())
	q := testQuery("TRUE")

	if c.key("count", "ws-1", q) == c.key("count", "ws-2", q) {
		t.Fatal("two workspaces share a cache key")
	}
	if c.key("count", "ws-1", q) == c.key("facets", "ws-1", q) {
		t.Fatal("the count and the facets share a cache key")
	}
}

func TestBumpInvalidatesEveryKeyForTheWorkspace(t *testing.T) {
	state := newFakeState()
	c := newAggregateCache(state)
	q := testQuery("TRUE")

	before := c.key("count", "ws-1", q)
	untouched := c.key("count", "ws-2", q)

	c.bump("ws-1")

	if after := c.key("count", "ws-1", q); after == before {
		t.Fatal("a write left the previous generation's keys reachable")
	}
	if still := c.key("count", "ws-2", q); still != untouched {
		t.Fatal("bumping one workspace invalidated another")
	}
}

func TestCountRoundTrips(t *testing.T) {
	c := newAggregateCache(newFakeState())
	q := testQuery("TRUE")

	if _, ok := c.getCount("ws-1", q); ok {
		t.Fatal("empty cache reported a hit")
	}

	c.setCount("ws-1", q, 42)

	got, ok := c.getCount("ws-1", q)
	if !ok || got != 42 {
		t.Fatalf("getCount() = (%d, %v), want (42, true)", got, ok)
	}
}

func TestZeroCountIsAHit(t *testing.T) {
	c := newAggregateCache(newFakeState())
	q := testQuery("TRUE")

	c.setCount("ws-1", q, 0)

	got, ok := c.getCount("ws-1", q)
	if !ok || got != 0 {
		t.Fatalf("getCount() = (%d, %v), want (0, true)", got, ok)
	}
}

func TestDisabledCacheAlwaysMisses(t *testing.T) {
	c := newAggregateCache(nil)
	q := testQuery("TRUE")

	c.setCount("ws-1", q, 7)
	if _, ok := c.getCount("ws-1", q); ok {
		t.Fatal("disabled cache reported a hit")
	}
	c.bump("ws-1")
}

func TestFailingCacheDegradesToMisses(t *testing.T) {
	state := newFakeState()
	state.fail = true
	c := newAggregateCache(state)
	q := testQuery("TRUE")

	c.setCount("ws-1", q, 7)
	if _, ok := c.getCount("ws-1", q); ok {
		t.Fatal("a failing cache reported a hit")
	}
}

func TestKeysAreBounded(t *testing.T) {
	c := newAggregateCache(newFakeState())
	long := strings.Repeat("leads.name ILIKE ? AND ", 400)
	if got := len(c.key("count", "ws-1", testQuery(long))); got > 128 {
		t.Errorf("key length = %d, want <= 128", got)
	}
}
