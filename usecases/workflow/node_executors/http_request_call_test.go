package node_executors

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/workflow"
)

func TestHTTPExecutor_WithoutAGuardedClientRefusesToSend(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	state := workflow.NewRunState()
	res, err := NewHTTPRequestExecutor(HTTPEgress{}, nil).Execute(httpNodeCtx("h1", map[string]interface{}{"url": srv.URL}, &state))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.NextNodeID != "err" || res.Output["success"] != false {
		t.Fatalf("next = %q output = %v, want the failure branch", res.NextNodeID, res.Output)
	}
	if hits.Load() != 0 {
		t.Fatal("a request went out without the guarded client")
	}
}

func TestHTTPExecutor_AHangUpAbortsTheRequest(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	call := &fakeVoiceCall{ended: make(chan struct{})}
	state := workflow.NewRunState()
	ctx := httpNodeCtx("h1", map[string]interface{}{"url": srv.URL, "timeout_seconds": float64(20)}, &state)
	ctx.Runtime = call
	time.AfterFunc(50*time.Millisecond, func() { close(call.ended) })

	started := time.Now()
	res, err := NewHTTPRequestExecutor(testEgress(), nil).Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("the request kept running %s after the caller hung up", elapsed)
	}
	if res.NextNodeID != "err" {
		t.Fatalf("next = %q, want the failure branch", res.NextNodeID)
	}
}

func TestHTTPExecutor_ASlowServerTakesTheTimeoutBranch(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	state := workflow.NewRunState()
	ctx := httpNodeCtx("h1", map[string]interface{}{"url": srv.URL, "timeout_seconds": float64(1)}, &state)
	ctx.Graph.Edges = append(ctx.Graph.Edges, workflow.Edge{Source: "h1", Target: "slow", Label: "timeout"})

	res, err := NewHTTPRequestExecutor(testEgress(), nil).Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.NextNodeID != "slow" {
		t.Fatalf("next = %q, want the timeout branch", res.NextNodeID)
	}
}

func TestHTTPExecutor_TimeoutIsBoundedSoNoRequestHoldsAWorkerForLong(t *testing.T) {
	cases := []struct {
		name   string
		config interface{}
		want   time.Duration
	}{
		{"unset", nil, defaultHTTPRequestTimeout},
		{"chosen", float64(5), 5 * time.Second},
		{"integer", 7, 7 * time.Second},
		{"above the cap", float64(3600), MaxHTTPRequestTimeout},
		{"negative", float64(-1), defaultHTTPRequestTimeout},
		{"text", "abc", defaultHTTPRequestTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := httpRequestTimeout(map[string]interface{}{"timeout_seconds": tc.config}); got != tc.want {
				t.Fatalf("timeout = %s, want %s", got, tc.want)
			}
		})
	}
}
