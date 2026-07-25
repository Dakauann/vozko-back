package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type mockRateLimiter struct {
	calls   int
	allowed bool
	err     error
}

func (m *mockRateLimiter) Allow(key string) (bool, time.Duration, error) {
	m.calls++
	if m.err != nil {
		return false, 0, m.err
	}
	if !m.allowed {
		return false, 30 * time.Second, nil
	}
	return true, 0, nil
}

func TestRateLimiter_AllowedRequest_PassesThrough(t *testing.T) {
	limiter := &mockRateLimiter{allowed: true}
	mw := NewRateLimiterMiddleware(limiter)

	called := false
	handler := mw.Validate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/auth/register", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected handler to be called when rate limit allows")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestRateLimiter_DeniedRequest_Returns429(t *testing.T) {
	limiter := &mockRateLimiter{allowed: false}
	mw := NewRateLimiterMiddleware(limiter)

	called := false
	handler := mw.Validate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/auth/register", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("handler should NOT be called when rate limit denies")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
}

func TestRateLimiter_Error_FailsClosed(t *testing.T) {
	limiter := &mockRateLimiter{err: errRedisDown}
	mw := NewRateLimiterMiddleware(limiter)

	called := false
	handler := mw.Validate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "5.6.7.8:1234"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("handler should NOT be called on limiter error (fail-closed)")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on error, got %d", rec.Code)
	}
}

func TestRateLimiter_ExtractsIP_FromXForwardedFor(t *testing.T) {
	var captured string
	limiter := &mockRateLimiter{allowed: true}

	originalAllow := limiter
	_ = originalAllow

	captureLimiter := &capturingRateLimiter{allowed: true}
	mw := NewRateLimiterMiddleware(captureLimiter)

	handler := mw.Validate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/auth/register", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.1.1")
	req.RemoteAddr = "127.0.0.1:9999"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	captured = captureLimiter.lastKey
	if captured != "10.0.0.1" {
		t.Fatalf("expected IP '10.0.0.1' from X-Forwarded-For, got '%s'", captured)
	}
}

func TestRateLimiter_ExtractsIP_FromXRealIP(t *testing.T) {
	captureLimiter := &capturingRateLimiter{allowed: true}
	mw := NewRateLimiterMiddleware(captureLimiter)

	handler := mw.Validate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.Header.Set("X-Real-IP", "203.0.113.42")
	req.RemoteAddr = "127.0.0.1:9999"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if captureLimiter.lastKey != "203.0.113.42" {
		t.Fatalf("expected IP '203.0.113.42' from X-Real-IP, got '%s'", captureLimiter.lastKey)
	}
}

var errRedisDown = http.ErrServerClosed

type capturingRateLimiter struct {
	allowed bool
	lastKey string
}

func (c *capturingRateLimiter) Allow(key string) (bool, time.Duration, error) {
	c.lastKey = key
	if !c.allowed {
		return false, 30 * time.Second, nil
	}
	return true, 0, nil
}

func TestRateLimiter_SkipPathPrefix_BypassesLimiterForWebhooks(t *testing.T) {
	limiter := &capturingRateLimiter{allowed: false}
	mw := NewRateLimiterMiddleware(limiter).SkipPathPrefixes("/webhooks/", "/health")

	called := false
	handler := mw.Validate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/webhooks/whatsapp", nil)
	req.Header.Set("X-Forwarded-For", "173.252.127.16")
	req.RemoteAddr = "127.0.0.1:9999"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("webhook handler should be called even when limiter would deny")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for webhook bypass, got %d", rec.Code)
	}
	if limiter.lastKey != "" {
		t.Fatalf("limiter should not be consulted for skipped path; got key %q", limiter.lastKey)
	}
}

func TestRateLimiter_SkipPathPrefix_WebhookBurstFromSameIP(t *testing.T) {
	limiter := &capturingRateLimiter{allowed: false}
	mw := NewRateLimiterMiddleware(limiter).SkipPathPrefixes("/webhooks/")

	handler := mw.Validate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 1000; i++ {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/whatsapp", nil)
		req.Header.Set("X-Forwarded-For", "173.252.127.16")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("burst request #%d got %d, expected 200", i, rec.Code)
		}
	}
	if limiter.lastKey != "" {
		t.Fatalf("limiter must not be consulted at all for webhooks; got key %q", limiter.lastKey)
	}
}

func TestRateLimiter_SkipPathPrefix_HealthBypasses(t *testing.T) {
	limiter := &capturingRateLimiter{allowed: false}
	mw := NewRateLimiterMiddleware(limiter).SkipPathPrefixes("/webhooks/", "/health")

	called := false
	handler := mw.Validate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called || rec.Code != http.StatusOK {
		t.Fatalf("/health should bypass limiter; called=%v code=%d", called, rec.Code)
	}
}

func TestRateLimiter_SkipPathPrefix_DoesNotAffectOtherPaths(t *testing.T) {
	limiter := &capturingRateLimiter{allowed: false}
	mw := NewRateLimiterMiddleware(limiter).SkipPathPrefixes("/webhooks/", "/health")

	called := false
	handler := mw.Validate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.Header.Set("X-Forwarded-For", "173.252.127.16")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("non-webhook path must still be rate limited when limiter denies")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on non-webhook path, got %d", rec.Code)
	}
	if limiter.lastKey != "173.252.127.16" {
		t.Fatalf("expected limiter to be consulted with client IP; got %q", limiter.lastKey)
	}
}

// fakeRateLimitMetrics captures IncRateLimited calls so we can assert the
// observability fires with the right labels (this is what powers the Grafana
// dashboard and pinpoints the offending office IP).
type fakeRateLimitMetrics struct {
	limiters []string
	ips      []string
	reasons  []string
}

func (f *fakeRateLimitMetrics) IncRateLimited(limiter, clientIP, reason string) {
	f.limiters = append(f.limiters, limiter)
	f.ips = append(f.ips, clientIP)
	f.reasons = append(f.reasons, reason)
}

func TestRateLimiter_Rejection_RecordsMetricWithLimiterIPAndReason(t *testing.T) {
	m := &fakeRateLimitMetrics{}
	mw := NewRateLimiterMiddleware(&capturingRateLimiter{allowed: false}).
		Named("global").WithMetrics(m)

	handler := mw.Validate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/conversations/whatsapp/abc/call-permission", nil)
	req.Header.Set("X-Real-IP", "179.191.107.18") // the shared office NAT
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if len(m.ips) != 1 {
		t.Fatalf("expected exactly one rejection recorded, got %d", len(m.ips))
	}
	if m.limiters[0] != "global" || m.ips[0] != "179.191.107.18" || m.reasons[0] != "limit_exceeded" {
		t.Fatalf("bad labels: limiter=%q ip=%q reason=%q (want global/179.191.107.18/limit_exceeded)",
			m.limiters[0], m.ips[0], m.reasons[0])
	}
}

func TestRateLimiter_LimiterError_RecordsErrorReason(t *testing.T) {
	m := &fakeRateLimitMetrics{}
	mw := NewRateLimiterMiddleware(&mockRateLimiter{err: errRedisDown}).Named("global").WithMetrics(m)
	handler := mw.Validate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/workspaces", nil)
	req.Header.Set("X-Real-IP", "203.0.113.5")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if len(m.reasons) != 1 || m.reasons[0] != "limiter_error" {
		t.Fatalf("expected one limiter_error rejection, got %v", m.reasons)
	}
}

func TestRateLimiter_AllowedRequest_RecordsNothing(t *testing.T) {
	m := &fakeRateLimitMetrics{}
	mw := NewRateLimiterMiddleware(&capturingRateLimiter{allowed: true}).Named("global").WithMetrics(m)
	handler := mw.Validate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/workspaces", nil)
	req.Header.Set("X-Real-IP", "203.0.113.5")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if len(m.ips) != 0 {
		t.Fatalf("allowed requests must not record a rejection, got %d", len(m.ips))
	}
}

func TestRateLimiter_SkippedPath_RecordsNothing(t *testing.T) {
	m := &fakeRateLimitMetrics{}
	mw := NewRateLimiterMiddleware(&capturingRateLimiter{allowed: false}).
		Named("global").WithMetrics(m).SkipPathPrefixes("/webhooks/")
	handler := mw.Validate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/webhooks/whatsapp", nil)
	req.Header.Set("X-Real-IP", "203.0.113.5")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if len(m.ips) != 0 {
		t.Fatalf("skipped paths must not record a rejection, got %d", len(m.ips))
	}
}

// countingLimiter enforces a real per-key budget so we can prove per-user isolation.
type countingLimiter struct {
	max    int
	counts map[string]int
}

func newCountingLimiter(max int) *countingLimiter {
	return &countingLimiter{max: max, counts: map[string]int{}}
}

func (c *countingLimiter) Allow(key string) (bool, time.Duration, error) {
	c.counts[key]++
	if c.counts[key] > c.max {
		return false, 30 * time.Second, nil
	}
	return true, 0, nil
}

// identify returns a fixed user id for a given "X-Test-User" header (stand-in for a
// verified token), and false when the header is absent (anonymous).
func testUserIdentity(r *http.Request) (string, bool) {
	if u := r.Header.Get("X-Test-User"); u != "" {
		return u, true
	}
	return "", false
}

func fireN(handler http.Handler, n int, hdr map[string]string) (ok, blocked int) {
	for i := 0; i < n; i++ {
		req := httptest.NewRequest(http.MethodGet, "/analysis/entry/abc", nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			blocked++
		} else {
			ok++
		}
	}
	return
}

// TestRateLimiter_PerUser_IndependentBudgetsSameIP is the core proof for the fix:
// many users behind ONE shared IP each get their own budget instead of colliding.
func TestRateLimiter_PerUser_IndependentBudgetsSameIP(t *testing.T) {
	lim := newCountingLimiter(5) // 5 requests per key
	mw := NewRateLimiterMiddleware(lim).Named("global").WithUserIdentity(testUserIdentity)
	handler := mw.Validate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	sameIP := "179.191.107.18" // the shared office NAT

	// Alice spends her whole budget from the shared IP.
	okA, blockedA := fireN(handler, 8, map[string]string{"X-Real-IP": sameIP, "X-Test-User": "alice"})
	// Bob, same IP, must be completely unaffected by Alice exhausting hers.
	okB, blockedB := fireN(handler, 5, map[string]string{"X-Real-IP": sameIP, "X-Test-User": "bob"})

	if okA != 5 || blockedA != 3 {
		t.Fatalf("alice: expected 5 ok + 3 blocked, got %d/%d", okA, blockedA)
	}
	if okB != 5 || blockedB != 0 {
		t.Fatalf("bob shares alice's IP but must have his OWN budget: expected 5 ok + 0 blocked, got %d/%d", okB, blockedB)
	}
}

// TestRateLimiter_Anonymous_FallsBackToPerIP proves anonymous traffic from one IP
// still shares a single per-IP budget (DoS protection preserved).
func TestRateLimiter_Anonymous_FallsBackToPerIP(t *testing.T) {
	lim := newCountingLimiter(5)
	mw := NewRateLimiterMiddleware(lim).Named("global").WithUserIdentity(testUserIdentity)
	handler := mw.Validate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// No X-Test-User → anonymous → keyed by IP; 8 requests from one IP → 3 blocked.
	ok, blocked := fireN(handler, 8, map[string]string{"X-Real-IP": "203.0.113.1"})
	if ok != 5 || blocked != 3 {
		t.Fatalf("anonymous same-IP flood must share one budget: expected 5 ok + 3 blocked, got %d/%d", ok, blocked)
	}
}

// TestRateLimiter_PerUser_SameUserAcrossIPsSharesBudget confirms a user is tracked
// by identity even when their IP changes (multiple devices / roaming).
func TestRateLimiter_PerUser_SameUserAcrossIPsSharesBudget(t *testing.T) {
	lim := newCountingLimiter(5)
	mw := NewRateLimiterMiddleware(lim).Named("global").WithUserIdentity(testUserIdentity)
	handler := mw.Validate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	ok1, _ := fireN(handler, 3, map[string]string{"X-Real-IP": "1.1.1.1", "X-Test-User": "carol"})
	ok2, blocked2 := fireN(handler, 5, map[string]string{"X-Real-IP": "2.2.2.2", "X-Test-User": "carol"})
	if ok1 != 3 || ok2 != 2 || blocked2 != 3 {
		t.Fatalf("same user across IPs shares one budget: got ok1=%d ok2=%d blocked2=%d (want 3, 2, 3)", ok1, ok2, blocked2)
	}
}

func TestRateLimiter_SkipPathPrefix_MatchesSubPaths(t *testing.T) {
	cases := []string{
		"/webhooks/whatsapp",
		"/webhooks/asaas",
		"/webhooks/uservoz/some-token",
	}
	for _, path := range cases {
		limiter := &capturingRateLimiter{allowed: false}
		mw := NewRateLimiterMiddleware(limiter).SkipPathPrefixes("/webhooks/")
		handler := mw.Validate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("path %q should bypass limiter, got %d", path, rec.Code)
		}
	}
}
