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

// stubFetcher is an in-memory generationUsageFetcher so the recovery path can be
// driven without a real HTTP endpoint.
type stubFetcher struct {
	pt, ct int
	ok     bool
	calls  int
	gotID  string
}

func (f *stubFetcher) FetchUsage(_ context.Context, id string) (int, int, bool) {
	f.calls++
	f.gotID = id
	return f.pt, f.ct, f.ok
}

// hangingSSEServer flushes the given chunks then holds the connection open until
// stop() is called — so a test can let the client receive some tokens and then
// cancel the request mid-stream, exactly like a timeout/abort in production.
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

// When a stream is cut before the inline usage chunk arrives, the turn must still
// be billed by recovering the real usage from the generation endpoint (by the id
// the stream carried). This is the core revenue-leak fix.
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

	// Read until we've seen the first token, then cancel mid-stream.
	gotToken := false
	go func() {
		for ev := range ch {
			if ev.Type == ai.StreamEventToken && !gotToken {
				gotToken = true
				cancel()
			}
		}
	}()

	// Give the goroutine time to drain + the billing recovery to run.
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

// EOF without a usage chunk (provider just never sent usage) is recovered too —
// the generation completed, so /generation has the counts.
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

// Recovery that fails (endpoint error / not found) must NOT bill — better to leak
// than to charge a fabricated amount.
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

// A recovery that returns zero tokens must NOT bill (zero-completion safety).
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

// Without a fetcher wired, behaviour is unchanged: a no-usage stream isn't billed
// (the legacy leak is preserved, not regressed) and nothing is fabricated.
func TestGenerateStream_NoFetcher_NoRecovery(t *testing.T) {
	srv := sseServer([]string{
		`{"id":"gen-n","choices":[{"index":0,"delta":{"content":"oi"}}]}`,
		`{"id":"gen-n","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	})
	defer srv.Close()

	pub := &capturingPub{}
	svc := newTestService(srv.URL, pub) // no usageFetcher

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

// Inline usage still wins: when the final usage chunk DOES arrive, we bill from it
// and never call the recovery fetcher.
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

// ---- httpGenerationFetcher direct tests ----------------------------------

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
	pt, ct, ok := f.FetchUsage(context.Background(), "gen-x")
	if !ok || pt != 1200 || ct != 1000 {
		t.Fatalf("FetchUsage = %d/%d ok=%v, want 1200/1000 true", pt, ct, ok)
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
	if _, _, ok := f.FetchUsage(context.Background(), "gen-x"); ok {
		t.Fatal("non-200 must yield ok=false")
	}
}

func TestHTTPGenerationFetcher_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	f := newHTTPGenerationFetcher("test-key", srv.URL)
	if _, _, ok := f.FetchUsage(context.Background(), "gen-x"); ok {
		t.Fatal("bad json must yield ok=false")
	}
}

func TestHTTPGenerationFetcher_GuardsEmptyInputs(t *testing.T) {
	f := newHTTPGenerationFetcher("", openRouterDefaultBaseURL)
	if _, _, ok := f.FetchUsage(context.Background(), "gen-x"); ok {
		t.Fatal("empty api key must yield ok=false")
	}
	f2 := newHTTPGenerationFetcher("k", openRouterDefaultBaseURL)
	if _, _, ok := f2.FetchUsage(context.Background(), "  "); ok {
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
