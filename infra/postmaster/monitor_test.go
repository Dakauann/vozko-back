package postmaster

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type fakeGoogle struct {
	tokenCalls atomic.Int32
	statsBody  string
	statsCode  int
	lastQuery  map[string]any
	lastPath   string
	lastAuth   string
}

func (f *fakeGoogle) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "refresh" || r.Form.Get("client_id") != "client" || r.Form.Get("client_secret") != "secret" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.tokenCalls.Add(1)
		_, _ = w.Write([]byte(`{"access_token":"access-1","expires_in":3600,"token_type":"Bearer"}`))
	})
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		f.lastPath = r.URL.Path
		f.lastAuth = r.Header.Get("Authorization")
		f.lastQuery = map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&f.lastQuery)
		if f.statsCode != 0 {
			w.WriteHeader(f.statsCode)
		}
		_, _ = w.Write([]byte(f.statsBody))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newMonitor(t *testing.T, srv *httptest.Server) *Monitor {
	t.Helper()
	m, err := New(Config{ClientID: "client", ClientSecret: "secret", RefreshToken: "refresh", TokenURL: srv.URL + "/token", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSpamRateReadsTheDailyStat(t *testing.T) {
	g := &fakeGoogle{statsBody: `{"domainStats":[{"name":"domains/loja.com.br/domainStats/x","metric":"spam_rate","date":{"year":2026,"month":10,"day":20},"value":{"doubleValue":0.0042}}]}`}
	m := newMonitor(t, g.server(t))
	day := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	stats, err := m.SpamRate(context.Background(), "Loja.com.br", day)
	if err != nil {
		t.Fatal(err)
	}
	if !stats.Available || stats.SpamRatePerMil != 4 || stats.Domain != "loja.com.br" || stats.Day != "2026-10-20" {
		t.Fatalf("stats = %+v", stats)
	}
	if g.lastPath != "/v2/domains/loja.com.br/domainStats:query" || g.lastAuth != "Bearer access-1" {
		t.Fatalf("path = %q auth = %q", g.lastPath, g.lastAuth)
	}
	defs := g.lastQuery["metricDefinitions"].([]any)
	base := defs[0].(map[string]any)["baseMetric"].(map[string]any)
	if base["standardMetric"] != "SPAM_RATE" || g.lastQuery["aggregationGranularity"] != "DAILY" {
		t.Fatalf("query = %v", g.lastQuery)
	}
	dates := g.lastQuery["timeQuery"].(map[string]any)["dateList"].(map[string]any)["dates"].([]any)
	date := dates[0].(map[string]any)
	if date["year"].(float64) != 2026 || date["month"].(float64) != 10 || date["day"].(float64) != 20 {
		t.Fatalf("date = %v", date)
	}
	if _, err := m.SpamRate(context.Background(), "loja.com.br", day); err != nil {
		t.Fatal(err)
	}
	if g.tokenCalls.Load() != 1 {
		t.Fatalf("token must be cached, calls = %d", g.tokenCalls.Load())
	}
}

func TestSpamRateUnavailable(t *testing.T) {
	cases := map[string]*fakeGoogle{
		"no stats":          {statsBody: `{}`},
		"not found":         {statsCode: http.StatusNotFound, statsBody: `{"error":{"code":404}}`},
		"other metric only": {statsBody: `{"domainStats":[{"metric":"other","value":{"doubleValue":0.5}}]}`},
	}
	for name, g := range cases {
		t.Run(name, func(t *testing.T) {
			stats, err := newMonitor(t, g.server(t)).SpamRate(context.Background(), "loja.com.br", time.Now())
			if err != nil || stats.Available {
				t.Fatalf("stats = %+v err = %v", stats, err)
			}
		})
	}
}

func TestSpamRateErrors(t *testing.T) {
	cases := map[string]*fakeGoogle{
		"permission denied": {statsCode: http.StatusForbidden, statsBody: `{"error":{"code":403}}`},
		"server error":      {statsCode: http.StatusInternalServerError},
		"garbage":           {statsBody: `nope`},
	}
	for name, g := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := newMonitor(t, g.server(t)).SpamRate(context.Background(), "loja.com.br", time.Now()); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer srv.Close()
	m, _ := New(Config{ClientID: "client", ClientSecret: "secret", RefreshToken: "refresh", TokenURL: srv.URL, APIBaseURL: srv.URL})
	if _, err := m.SpamRate(context.Background(), "loja.com.br", time.Now()); err == nil {
		t.Fatal("token failure must fail")
	}
}

func TestNewRequiresCredentials(t *testing.T) {
	if _, err := New(Config{ClientID: "a", ClientSecret: "b"}); err == nil {
		t.Fatal("missing refresh token must fail")
	}
	m, err := New(Config{ClientID: "a", ClientSecret: "b", RefreshToken: "c"})
	if err != nil || m.Name() != "google_postmaster" || m.tokenURL != defaultTokenURL || m.base != defaultAPIBaseURL {
		t.Fatalf("monitor = %+v err = %v", m, err)
	}
}
