package mercadolivre

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/cache"
	mldomain "vozko/domain/mercadolivre"
)

type limiterCall struct {
	prefix string
	key    string
}

type fakeLimiters struct {
	mu      sync.Mutex
	calls   []limiterCall
	budgets map[string]int
	windows map[string]time.Duration
	deny    map[string]time.Duration
	denyFor map[string]int
	fail    map[string]error
}

func newFakeLimiters() *fakeLimiters {
	return &fakeLimiters{
		budgets: map[string]int{},
		windows: map[string]time.Duration{},
		deny:    map[string]time.Duration{},
		denyFor: map[string]int{},
		fail:    map[string]error{},
	}
}

func (f *fakeLimiters) factory() cache.RateLimiterFactory {
	return func(prefix string, maxRequests int, window time.Duration) cache.RateLimiter {
		f.mu.Lock()
		f.budgets[prefix] = maxRequests
		f.windows[prefix] = window
		f.mu.Unlock()
		return &fakeLimiter{owner: f, prefix: prefix}
	}
}

func (f *fakeLimiters) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.key)
	}
	return out
}

type fakeLimiter struct {
	owner  *fakeLimiters
	prefix string
}

func (l *fakeLimiter) Allow(key string) (bool, time.Duration, error) {
	f := l.owner
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, limiterCall{prefix: l.prefix, key: key})
	if err, ok := f.fail[key]; ok {
		return false, 0, err
	}
	if retry, ok := f.deny[key]; ok {
		if n, limited := f.denyFor[key]; !limited || n > 0 {
			if limited {
				f.denyFor[key] = n - 1
			}
			return false, retry, nil
		}
	}
	return true, 0, nil
}

type recordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   string
}

type fakeServer struct {
	t        *testing.T
	mu       sync.Mutex
	requests []recordedRequest
	handler  func(w http.ResponseWriter, r *http.Request, attempt int)
	srv      *httptest.Server
}

func newFakeServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, attempt int)) *fakeServer {
	t.Helper()
	fs := &fakeServer{t: t, handler: handler}
	fs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fs.mu.Lock()
		fs.requests = append(fs.requests, recordedRequest{
			Method: r.Method,
			Path:   r.URL.EscapedPath(),
			Query:  r.URL.Query(),
			Header: r.Header.Clone(),
			Body:   string(body),
		})
		attempt := len(fs.requests)
		fs.mu.Unlock()
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		fs.handler(w, r, attempt)
	}))
	t.Cleanup(fs.srv.Close)
	return fs
}

func (fs *fakeServer) count() int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return len(fs.requests)
}

func (fs *fakeServer) last() recordedRequest {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.requests) == 0 {
		fs.t.Fatal("no request reached the server")
	}
	return fs.requests[len(fs.requests)-1]
}

func newTestClient(t *testing.T, fs *fakeServer, limiters *fakeLimiters) *Client {
	t.Helper()
	c, err := NewClient(Config{
		BaseURL:            fs.srv.URL,
		HTTPClient:         fs.srv.Client(),
		RateLimiterFactory: limiters.factory(),
		Backoff:            func(int) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func jsonReply(status int, body string) func(w http.ResponseWriter, r *http.Request, attempt int) {
	return func(w http.ResponseWriter, r *http.Request, attempt int) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(raw)
}

var sellerCreds = mldomain.Credentials{UserID: "415458330", AccessToken: "APP_USR-123456-090515-secret-415458330"}

func assertNoTokenInQuery(t *testing.T, r recordedRequest) {
	t.Helper()
	for k, vs := range r.Query {
		if strings.EqualFold(k, "access_token") {
			t.Fatalf("token sent in the query: %v", r.Query)
		}
		for _, v := range vs {
			if strings.Contains(v, "secret") {
				t.Fatalf("token value leaked into the query: %v", r.Query)
			}
		}
	}
}

func assertBearer(t *testing.T, r recordedRequest) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer "+sellerCreds.AccessToken {
		t.Fatalf("Authorization = %q", got)
	}
	assertNoTokenInQuery(t, r)
}
