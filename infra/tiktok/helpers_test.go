package tiktok

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/cache"
	ttdomain "vozko/domain/tiktok"
)

const (
	testAuthorizeURL = "https://www.tiktok.com/v2/auth/authorize?client_key=7043848340043202562&scope=user.info.basic%2Cvideo.list&redirect_uri=https%3A%2F%2Fapi.example.com%2Foauth%2Ftiktok%2Fcallback%2F"
	testRedirectURI  = "https://api.example.com/oauth/tiktok/callback/"
)

var testCreds = ttdomain.Credentials{OpenID: "_000abPL1cJIdVefgDhGijkl2mFn3AB0abY4", AccessToken: "act.secret-token"}

type limiterCall struct {
	prefix string
	key    string
}

type fakeLimiter struct {
	prefix string
	owner  *fakeLimiters
}

func (l fakeLimiter) Allow(key string) (bool, time.Duration, error) {
	l.owner.mu.Lock()
	defer l.owner.mu.Unlock()
	l.owner.calls = append(l.owner.calls, limiterCall{prefix: l.prefix, key: key})
	if l.owner.err != nil && (l.owner.failPrefix == "" || l.owner.failPrefix == l.prefix) {
		return false, 0, l.owner.err
	}
	if l.owner.refuse != "" && l.owner.refuse == l.prefix {
		return false, time.Hour, nil
	}
	return true, 0, nil
}

type fakeLimiters struct {
	mu         sync.Mutex
	calls      []limiterCall
	budgets    map[string]int
	err        error
	failPrefix string
	refuse     string
}

func (f *fakeLimiters) factory() cache.RateLimiterFactory {
	return func(prefix string, max int, window time.Duration) cache.RateLimiter {
		f.mu.Lock()
		if f.budgets == nil {
			f.budgets = map[string]int{}
		}
		f.budgets[prefix] = max
		f.mu.Unlock()
		return fakeLimiter{prefix: prefix, owner: f}
	}
}

func (f *fakeLimiters) prefixesCalled() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.prefix)
	}
	return out
}

type recordedRequest struct {
	Method string
	Path   string
	Query  map[string][]string
	Header http.Header
	Body   []byte
}

type fakeTikTok struct {
	t        *testing.T
	mu       sync.Mutex
	requests []recordedRequest
	handle   func(w http.ResponseWriter, r recordedRequest, attempt int)
	server   *httptest.Server
}

func newFakeTikTok(t *testing.T, handle func(w http.ResponseWriter, r recordedRequest, attempt int)) *fakeTikTok {
	f := &fakeTikTok{t: t, handle: handle}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec := recordedRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Header: r.Header.Clone(), Body: body}
		f.mu.Lock()
		f.requests = append(f.requests, rec)
		attempt := 0
		for _, prev := range f.requests {
			if prev.Path == rec.Path {
				attempt++
			}
		}
		f.mu.Unlock()
		f.handle(w, rec, attempt)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeTikTok) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeTikTok) last() recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func writeOK(w http.ResponseWriter, data any) {
	raw, _ := json.Marshal(data)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"code":0,"message":"OK","request_id":"req-1","data":` + string(raw) + `}`))
}

func writeCode(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"code":` + itoa(code) + `,"message":"` + message + `","request_id":"req-err","data":{}}`))
}

func itoa(n int) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}

func newTestClient(t *testing.T, base string, limiters *fakeLimiters) *Client {
	t.Helper()
	if limiters == nil {
		limiters = &fakeLimiters{}
	}
	c, err := NewClient(Config{
		BaseURL:            base,
		AppID:              "7043848340043202562",
		AppSecret:          "app-secret",
		RedirectURI:        testRedirectURI,
		AuthorizeURL:       testAuthorizeURL,
		RateLimiterFactory: limiters.factory(),
		Backoff:            func(int) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func decodeBody(t *testing.T, r recordedRequest) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(r.Body, &out); err != nil {
		t.Fatalf("request body %q: %v", string(r.Body), err)
	}
	return out
}

func containsAll(haystack string, needles ...string) bool {
	for _, n := range needles {
		if !strings.Contains(haystack, n) {
			return false
		}
	}
	return true
}
