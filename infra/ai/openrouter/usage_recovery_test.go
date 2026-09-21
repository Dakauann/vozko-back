package openrouter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vozko/domain/ai"
)

type stubFetcher struct {
	pt, ct     int
	costMicros int64
	ok         bool
	calls      int
	gotID      string
}

func (f *stubFetcher) FetchUsage(_ context.Context, id string) (int, int, int64, bool) {
	f.calls++
	f.gotID = id
	return f.pt, f.ct, f.costMicros, f.ok
}

func hangingSSEServer(chunks []string) (*httptest.Server, func()) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			if flusher != nil {
				flusher.Flush()
			}
		}
		<-release
	}))
	return srv, func() { close(release) }
}

func TestGenerateStream_CutStream_RecoversUsageAndBills(t *testing.T) {
	srv, release := hangingSSEServer([]string{
		`{"id":"gen-cut-1","choices":[{"index":0,"delta":{"content":"pensando"}}]}`,
	})
	defer srv.Close()
	defer release()

	pub := &capturingPub{}
	svc := newTestService(srv.URL, pub)
	fetcher := &stubFetcher{pt: 1200, ct: 1000, ok: true}
	svc.usageFetcher = fetcher

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := svc.GenerateStream(ctx, ai.GenerateInput{
		Model:       "z-ai/glm-5.2",
		WorkspaceID: "ws-1",
		Messages:    []ai.Message{{Role: ai.RoleUser, Content: "crie um fluxo"}},
	})
	if err != nil {
		t.Fatalf("GenerateStream: %v", err)
	}

	gotToken := false
	go func() {
		for ev := range ch {
			if ev.Type == ai.StreamEventToken && !gotToken {
				gotToken = true
				cancel()
			}
		}
	}()

	deadline := time.After(2 * time.Second)
	for {
		if len(pub.events()) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("no billing event after a cut stream (recovery did not run); fetcher.calls=%d", fetcher.calls)
		case <-time.After(10 * time.Millisecond):
		}
	}

	if fetcher.calls != 1 {
		t.Fatalf("usage fetcher called %d times, want 1", fetcher.calls)
	}
	if fetcher.gotID != "gen-cut-1" {
		t.Fatalf("recovery used generation id %q, want gen-cut-1", fetcher.gotID)
	}
	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("want exactly 1 billing event from recovery, got %d", len(events))
	}
	e := events[0]
	if e.WorkspaceID != "ws-1" || e.Model != "z-ai/glm-5.2" {
		t.Errorf("unexpected billing target: %+v", e)
	}
	if e.PromptTokens != 1200 || e.CompletionTokens != 1000 {
		t.Errorf("recovered billing = %d/%d, want 1200/1000", e.PromptTokens, e.CompletionTokens)
	}
}

func TestGenerateStream_EOFNoUsage_RecoversAndBills(t *testing.T) {
	srv := sseServer([]string{
		`{"id":"gen-eof-1","choices":[{"index":0,"delta":{"content":"oi"}}]}`,
		`{"id":"gen-eof-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	})
	defer srv.Close()

	pub := &capturingPub{}
	svc := newTestService(srv.URL, pub)
	svc.usageFetcher = &stubFetcher{pt: 30, ct: 12, ok: true}

	ch, err := svc.GenerateStream(context.Background(), ai.GenerateInput{
		Model:       "z-ai/glm-5.2",
		WorkspaceID: "ws-1",
		Messages:    []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})
	if err != nil {
		t.Fatalf("GenerateStream: %v", err)
	}
	drain(t, ch)

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("want 1 recovered billing event, got %d", len(events))
	}
	if events[0].PromptTokens != 30 || events[0].CompletionTokens != 12 {
		t.Errorf("recovered billing = %d/%d, want 30/12", events[0].PromptTokens, events[0].CompletionTokens)
	}
}

func TestGenerateStream_RecoveryFails_DoesNotBill(t *testing.T) {
	srv := sseServer([]string{
		`{"id":"gen-x","choices":[{"index":0,"delta":{"content":"oi"}}]}`,
		`{"id":"gen-x","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	})
	defer srv.Close()

	pub := &capturingPub{}
	svc := newTestService(srv.URL, pub)
	svc.usageFetcher = &stubFetcher{ok: false}

	ch, _ := svc.GenerateStream(context.Background(), ai.GenerateInput{
		Model:       "z-ai/glm-5.2",
		WorkspaceID: "ws-1",
		Messages:    []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})
	drain(t, ch)
	if got := len(pub.events()); got != 0 {
		t.Fatalf("failed recovery must not bill, got %d events", got)
	}
}

func TestGenerateStream_RecoveryZeroTokens_DoesNotBill(t *testing.T) {
	srv := sseServer([]string{
		`{"id":"gen-z","choices":[{"index":0,"delta":{"content":"oi"}}]}`,
		`{"id":"gen-z","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	})
	defer srv.Close()

	pub := &capturingPub{}
	svc := newTestService(srv.URL, pub)
	svc.usageFetcher = &stubFetcher{pt: 0, ct: 0, ok: true}

	ch, _ := svc.GenerateStream(context.Background(), ai.GenerateInput{
		Model:       "z-ai/glm-5.2",
		WorkspaceID: "ws-1",
		Messages:    []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})
	drain(t, ch)
	if got := len(pub.events()); got != 0 {
		t.Fatalf("zero-token recovery must not bill, got %d events", got)
	}
}

func TestGenerateStream_NoFetcher_NoRecovery(t *testing.T) {
	srv := sseServer([]string{
		`{"id":"gen-n","choices":[{"index":0,"delta":{"content":"oi"}}]}`,
		`{"id":"gen-n","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	})
	defer srv.Close()

	pub := &capturingPub{}
	svc := newTestService(srv.URL, pub)

	ch, _ := svc.GenerateStream(context.Background(), ai.GenerateInput{
		Model:       "z-ai/glm-5.2",
		WorkspaceID: "ws-1",
		Messages:    []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})
	drain(t, ch)
	if got := len(pub.events()); got != 0 {
		t.Fatalf("no fetcher → no bill, got %d events", got)
	}
}

func TestGenerateStream_InlineUsage_SkipsRecovery(t *testing.T) {
	srv := sseServer([]string{
		`{"id":"gen-i","choices":[{"index":0,"delta":{"content":"oi"}}]}`,
		`{"id":"gen-i","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`,
	})
	defer srv.Close()

	pub := &capturingPub{}
	svc := newTestService(srv.URL, pub)
	fetcher := &stubFetcher{pt: 999, ct: 999, ok: true}
	svc.usageFetcher = fetcher

	ch, _ := svc.GenerateStream(context.Background(), ai.GenerateInput{
		Model:       "z-ai/glm-5.2",
		WorkspaceID: "ws-1",
		Messages:    []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})
	drain(t, ch)

	if fetcher.calls != 0 {
		t.Fatalf("recovery must not run when inline usage is present, calls=%d", fetcher.calls)
	}
	events := pub.events()
	if len(events) != 1 || events[0].PromptTokens != 7 || events[0].CompletionTokens != 3 {
		t.Fatalf("want inline billing 7/3, got %+v", events)
	}
}

func TestHTTPGenerationFetcher_Success(t *testing.T) {
	var gotPath, gotID, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotID = r.URL.Query().Get("id")
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":{"tokens_prompt":1200,"tokens_completion":1000,"native_tokens_prompt":1100}}`))
	}))
	defer srv.Close()

	f := newHTTPGenerationFetcher("test-key", srv.URL)
	pt, ct, costMicros, ok := f.FetchUsage(context.Background(), "gen-x")
	if !ok || pt != 1200 || ct != 1000 {
		t.Fatalf("FetchUsage = %d/%d ok=%v, want 1200/1000 true", pt, ct, ok)
	}
	if costMicros != 0 {
		t.Fatalf("costMicros = %d, want 0 when the payload carries no total_cost", costMicros)
	}
	if gotPath != "/generation" || gotID != "gen-x" || gotAuth != "Bearer test-key" {
		t.Fatalf("unexpected request: path=%q id=%q auth=%q", gotPath, gotID, gotAuth)
	}
}

func TestHTTPGenerationFetcher_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	f := newHTTPGenerationFetcher("test-key", srv.URL)
	if _, _, _, ok := f.FetchUsage(context.Background(), "gen-x"); ok {
		t.Fatal("non-200 must yield ok=false")
	}
}

func TestHTTPGenerationFetcher_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	f := newHTTPGenerationFetcher("test-key", srv.URL)
	if _, _, _, ok := f.FetchUsage(context.Background(), "gen-x"); ok {
		t.Fatal("bad json must yield ok=false")
	}
}

func TestHTTPGenerationFetcher_GuardsEmptyInputs(t *testing.T) {
	f := newHTTPGenerationFetcher("", openRouterDefaultBaseURL)
	if _, _, _, ok := f.FetchUsage(context.Background(), "gen-x"); ok {
		t.Fatal("empty api key must yield ok=false")
	}
	f2 := newHTTPGenerationFetcher("k", openRouterDefaultBaseURL)
	if _, _, _, ok := f2.FetchUsage(context.Background(), "  "); ok {
		t.Fatal("empty generation id must yield ok=false")
	}
}

func TestNewHTTPGenerationFetcher_DefaultsBaseURL(t *testing.T) {
	f := newHTTPGenerationFetcher("k", "")
	if f.baseURL != openRouterDefaultBaseURL {
		t.Fatalf("base url = %q, want default %q", f.baseURL, openRouterDefaultBaseURL)
	}
	f2 := newHTTPGenerationFetcher("k", "https://example.com/api/v1/")
	if f2.baseURL != "https://example.com/api/v1" {
		t.Fatalf("trailing slash not trimmed: %q", f2.baseURL)
	}
}
