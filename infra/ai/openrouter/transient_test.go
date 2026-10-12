package openrouter

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vozko/domain/ai"
)

func statusServer(status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"error":{"code":%d,"message":"status %d"}}`, status, status)
	}))
}

func streamFailureOf(t *testing.T, baseURL string, pub *capturingPub) error {
	t.Helper()
	svc := newTestService(baseURL, pub)
	ch, err := svc.GenerateStream(context.Background(), ai.GenerateInput{
		Model: "x-ai/grok-4.7", WorkspaceID: "ws-1",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})
	if err != nil {
		return err
	}
	_, failure := streamOutcome(ch)
	svc.recoveries.Wait()
	return failure
}

func TestPassingProviderStatusesAreMarkedTransient(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 524, 529} {
		srv := statusServer(status)
		failure := streamFailureOf(t, srv.URL, &capturingPub{})
		srv.Close()
		if !errors.Is(failure, ai.ErrProviderTransient) {
			t.Errorf("status %d: error = %v, want it marked transient", status, failure)
		}
	}
}

func TestRefusalsAreNeverMarkedTransient(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden, http.StatusNotFound} {
		srv := statusServer(status)
		failure := streamFailureOf(t, srv.URL, &capturingPub{})
		srv.Close()
		if failure == nil || errors.Is(failure, ai.ErrProviderTransient) {
			t.Errorf("status %d: error = %v, want a permanent failure", status, failure)
		}
	}
}

func TestAConnectionDroppedBeforeAnyAnswerIsTransientAndBillsNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer srv.Close()

	pub := &capturingPub{}
	failure := streamFailureOf(t, srv.URL, pub)
	if !errors.Is(failure, ai.ErrProviderTransient) {
		t.Fatalf("error = %v, want it marked transient", failure)
	}
	if got := len(pub.events()); got != 0 {
		t.Fatalf("no answer means no generation to bill, got %d events", got)
	}
}

func TestAnUnreachableHostIsTransient(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()

	failure := streamFailureOf(t, "http://"+addr, &capturingPub{})
	if !errors.Is(failure, ai.ErrProviderTransient) {
		t.Fatalf("error = %v, want it marked transient", failure)
	}
}

func TestAStreamCutMidwayIsTransientAndStillBillsWhatTheProviderReports(t *testing.T) {
	srv := sseServer([]string{
		`{"id":"gen-cut","choices":[{"index":0,"delta":{"content":"Vou montar"}}]}`,
	})
	defer srv.Close()

	pub := &capturingPub{}
	svc := newTestService(srv.URL, pub)
	svc.usageFetcher = &notYetIndexedFetcher{pt: 120, ct: 4}
	svc.usageRetryDelays = []time.Duration{0}
	ch, _ := svc.GenerateStream(context.Background(), ai.GenerateInput{
		Model: "x-ai/grok-4.7", WorkspaceID: "ws-1", BillingReference: "aichat:t1",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})
	_, failure := streamOutcome(ch)
	svc.recoveries.Wait()

	if !errors.Is(failure, ai.ErrProviderTransient) || !errors.Is(failure, ai.ErrStreamIncomplete) {
		t.Fatalf("error = %v, want a transient incomplete stream", failure)
	}
	events := pub.events()
	if len(events) != 1 || events[0].PromptTokens != 120 || events[0].CompletionTokens != 4 {
		t.Fatalf("the cut generation must be billed once with the provider's own usage, got %+v", events)
	}
}

func TestACancelledCallIsNotTransient(t *testing.T) {
	srv := sseServer(nil)
	defer srv.Close()
	svc := newTestService(srv.URL, &capturingPub{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch, err := svc.GenerateStream(ctx, ai.GenerateInput{
		Model: "x-ai/grok-4.7", WorkspaceID: "ws-1",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})
	failure := err
	if err == nil {
		_, failure = streamOutcome(ch)
	}
	if errors.Is(failure, ai.ErrProviderTransient) {
		t.Fatalf("a cancelled call must never be retried, got %v", failure)
	}
}
