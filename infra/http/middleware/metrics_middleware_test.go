package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type stubRecorder struct {
	mu sync.Mutex

	latencyObservations []latencyObs
	requests            []reqObs
	inFlightInc         []inFlightObs
	inFlightDec         []inFlightObs
}

type latencyObs struct {
	method  string
	path    string
	status  string
	elapsed time.Duration
}

type reqObs struct {
	method string
	path   string
	status string
}

type inFlightObs struct {
	method string
	path   string
}

func (s *stubRecorder) ObserveHTTPLatency(method, path, status string, elapsed time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latencyObservations = append(s.latencyObservations, latencyObs{method, path, status, elapsed})
}

func (s *stubRecorder) IncHTTPRequests(method, path, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, reqObs{method, path, status})
}

func (s *stubRecorder) IncHTTPInFlight(method, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inFlightInc = append(s.inFlightInc, inFlightObs{method, path})
}

func (s *stubRecorder) DecHTTPInFlight(method, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inFlightDec = append(s.inFlightDec, inFlightObs{method, path})
}

func TestHTTPMetrics_Record(t *testing.T) {
	rec := &stubRecorder{}
	mw := NewHTTPMetrics(rec, HTTPMetricsConfig{NormalizePaths: true})

	handler := mw.Record(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/campaigns/550e8400-e29b-41d4-a716-446655440000", nil)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)

	if len(rec.latencyObservations) != 1 {
		t.Fatalf("expected 1 latency observation, got %d", len(rec.latencyObservations))
	}
	obs := rec.latencyObservations[0]
	if obs.method != http.MethodPost {
		t.Errorf("expected method POST, got %s", obs.method)
	}
	if obs.path != "/api/v1/campaigns/:id" {
		t.Errorf("expected normalized path, got %s", obs.path)
	}
	if obs.status != "201" {
		t.Errorf("expected status 201, got %s", obs.status)
	}

	if len(rec.requests) != 1 {
		t.Fatalf("expected 1 request observation, got %d", len(rec.requests))
	}
	reqObs := rec.requests[0]
	if reqObs.method != http.MethodPost {
		t.Errorf("expected method POST, got %s", reqObs.method)
	}
	if reqObs.path != "/api/v1/campaigns/:id" {
		t.Errorf("expected normalized path, got %s", reqObs.path)
	}
	if reqObs.status != "201" {
		t.Errorf("expected status 201, got %s", reqObs.status)
	}

	if len(rec.inFlightInc) != 1 || len(rec.inFlightDec) != 1 {
		t.Fatalf("expected 1 in-flight inc and 1 dec, got inc=%d dec=%d", len(rec.inFlightInc), len(rec.inFlightDec))
	}
}

func TestHTTPMetrics_SkipPrefixes(t *testing.T) {
	rec := &stubRecorder{}
	mw := NewHTTPMetrics(rec, HTTPMetricsConfig{
		SkipPrefixes: []string{"/health", "/metrics"},
	})

	handler := mw.Record(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{"/health", "/metrics", "/metrics/foo"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rw := httptest.NewRecorder()
		handler.ServeHTTP(rw, req)
	}

	if len(rec.latencyObservations) != 0 {
		t.Errorf("expected 0 observations for skipped prefixes, got %d", len(rec.latencyObservations))
	}
	if len(rec.requests) != 0 {
		t.Errorf("expected 0 requests for skipped prefixes, got %d", len(rec.requests))
	}
}

func TestHTTPMetrics_PathNormalization(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/api/v1/campaigns/550e8400-e29b-41d4-a716-446655440000/items", "/api/v1/campaigns/:id/items"},
		{"/api/v1/agents/42", "/api/v1/agents/42"},
		{"/api/v1/agents/123456", "/api/v1/agents/:id"},
		{"/api/v1/workspaces/abc123def4567890/settings", "/api/v1/workspaces/:id/settings"},
		{"/api/v1/calls", "/api/v1/calls"},
		{"/health", "/health"},
		{"/", "/"},
		{"/api/v1/campaigns/abc-123-def", "/api/v1/campaigns/abc-123-def"},
	}

	for _, tt := range tests {
		got := normalizePath(tt.input)
		if got != tt.expected {
			t.Errorf("normalizePath(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestHTTPMetrics_DefaultStatusOK(t *testing.T) {
	rec := &stubRecorder{}
	mw := NewHTTPMetrics(rec)

	handler := mw.Record(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/calls", nil)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)

	if len(rec.requests) != 1 {
		t.Fatalf("expected 1 request observation, got %d", len(rec.requests))
	}
	if rec.requests[0].status != "200" {
		t.Errorf("expected status 200 for implicit Write, got %s", rec.requests[0].status)
	}
}

func TestHTTPMetrics_ConcurrentRequests(t *testing.T) {
	rec := &stubRecorder{}
	mw := NewHTTPMetrics(rec)

	handler := mw.Record(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/calls", nil)
			rw := httptest.NewRecorder()
			handler.ServeHTTP(rw, req)
		}()
	}
	wg.Wait()

	if len(rec.requests) != 50 {
		t.Errorf("expected 50 request observations, got %d", len(rec.requests))
	}
	if len(rec.latencyObservations) != 50 {
		t.Errorf("expected 50 latency observations, got %d", len(rec.latencyObservations))
	}
}

func TestHTTPMetrics_NilRecorderNoPanic(t *testing.T) {
	mw := NewHTTPMetrics(nil)

	handler := mw.Record(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/calls", nil)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rw.Code)
	}
}

func TestLooksLikeID(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"550e8400-e29b-41d4-a716-446655440000", true},
		{"abc123def456789", true},
		{"123456", true},
		{"42", false},
		{"calls", false},
		{"api", false},
		{"a1b2c3d4e5f6g7h8", true},
	}

	for _, tt := range tests {
		got := looksLikeID(tt.input)
		if got != tt.want {
			t.Errorf("looksLikeID(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestHTTPMetrics_InFlightGauge(t *testing.T) {
	rec := &stubRecorder{}
	mw := NewHTTPMetrics(rec)

	var inFlightCount int64

	handler := mw.Record(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&inFlightCount, 1)
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt64(&inFlightCount, -1)
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/calls", nil)
			rw := httptest.NewRecorder()
			handler.ServeHTTP(rw, req)
		}()
	}
	wg.Wait()

	if len(rec.inFlightInc) != 5 {
		t.Errorf("expected 5 in-flight inc, got %d", len(rec.inFlightInc))
	}
	if len(rec.inFlightDec) != 5 {
		t.Errorf("expected 5 in-flight dec, got %d", len(rec.inFlightDec))
	}
}

func TestResponseWriter_HijackPassthrough(t *testing.T) {
	inner := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: inner}

	var _ http.Hijacker = rw
}

func TestResponseWriter_FlushPassthrough(t *testing.T) {
	inner := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: inner}

	var _ http.Flusher = rw
}
