package openrouter

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/ai"
)

type notYetIndexedFetcher struct {
	mu       sync.Mutex
	misses   int
	calls    int
	pt, ct   int
	lastSeen string
}

func (f *notYetIndexedFetcher) FetchUsage(_ context.Context, id string) (int, int, int64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastSeen = id
	if f.calls <= f.misses {
		return 0, 0, 0, false
	}
	return f.pt, f.ct, 0, true
}

func streamOutcome(ch <-chan ai.StreamEvent) (done *ai.StreamEvent, failure error) {
	for ev := range ch {
		switch ev.Type {
		case ai.StreamEventDone:
			captured := ev
			done = &captured
		case ai.StreamEventError:
			failure = ev.Error
		}
	}
	return done, failure
}

func TestRecoveryRetriesUntilTheGenerationIsIndexed(t *testing.T) {
	srv := sseServer([]string{
		`{"id":"gen-late","choices":[{"index":0,"delta":{"content":"oi"}}]}`,
		`{"id":"gen-late","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	})
	defer srv.Close()

	pub := &capturingPub{}
	svc := newTestService(srv.URL, pub)
	fetcher := &notYetIndexedFetcher{misses: 2, pt: 40, ct: 9}
	svc.usageFetcher = fetcher
	svc.usageRetryDelays = []time.Duration{0, time.Millisecond, time.Millisecond, time.Millisecond}

	ch, err := svc.GenerateStream(context.Background(), ai.GenerateInput{
		Model: "x-ai/grok-4.7", WorkspaceID: "ws-1",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	streamOutcome(ch)
	svc.recoveries.Wait()

	if fetcher.calls != 3 {
		t.Fatalf("fetcher called %d times, want 3 (two misses, then the usage)", fetcher.calls)
	}
	events := pub.events()
	if len(events) != 1 || events[0].PromptTokens != 40 || events[0].CompletionTokens != 9 {
		t.Fatalf("billing = %+v, want one event 40/9", events)
	}
}

func TestRecoveryGivesUpAfterTheScheduleWithoutBilling(t *testing.T) {
	srv := sseServer([]string{
		`{"id":"gen-gone","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	})
	defer srv.Close()

	pub := &capturingPub{}
	svc := newTestService(srv.URL, pub)
	fetcher := &notYetIndexedFetcher{misses: 99}
	svc.usageFetcher = fetcher
	svc.usageRetryDelays = []time.Duration{0, time.Millisecond, time.Millisecond}

	ch, _ := svc.GenerateStream(context.Background(), ai.GenerateInput{
		Model: "x-ai/grok-4.7", WorkspaceID: "ws-1",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})
	streamOutcome(ch)
	svc.recoveries.Wait()

	if fetcher.calls != 3 {
		t.Fatalf("fetcher called %d times, want one per scheduled attempt (3)", fetcher.calls)
	}
	if got := len(pub.events()); got != 0 {
		t.Fatalf("unrecovered usage must not bill, got %d events", got)
	}
}

func TestStreamThatStopsBeforeTheProviderFinishesIsAnError(t *testing.T) {
	srv := sseServer([]string{
		`{"id":"gen-cut","choices":[{"index":0,"delta":{"reasoning":"procurando o atendente"}}]}`,
	})
	defer srv.Close()

	svc := newTestService(srv.URL, &capturingPub{})
	ch, _ := svc.GenerateStream(context.Background(), ai.GenerateInput{
		Model: "x-ai/grok-4.7", WorkspaceID: "ws-1",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})
	done, failure := streamOutcome(ch)
	svc.recoveries.Wait()

	if done != nil {
		t.Fatalf("a cut stream reported done: %+v", done)
	}
	if !errors.Is(failure, ai.ErrStreamIncomplete) {
		t.Fatalf("error = %v, want ai.ErrStreamIncomplete", failure)
	}
}

func TestProviderErrorFinishIsAnError(t *testing.T) {
	srv := sseServer([]string{
		`{"id":"gen-err","choices":[{"index":0,"delta":{},"finish_reason":"error"}],"error":{"code":502,"message":"upstream"}}`,
	})
	defer srv.Close()

	svc := newTestService(srv.URL, &capturingPub{})
	ch, _ := svc.GenerateStream(context.Background(), ai.GenerateInput{
		Model: "x-ai/grok-4.7", WorkspaceID: "ws-1",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})
	done, failure := streamOutcome(ch)
	svc.recoveries.Wait()

	if done != nil || !errors.Is(failure, ai.ErrStreamIncomplete) {
		t.Fatalf("done = %+v, error = %v; want ai.ErrStreamIncomplete", done, failure)
	}
}

func TestUsageOnlyEndingStillCountsAsFinished(t *testing.T) {
	srv := sseServer([]string{
		`{"id":"gen-u","choices":[{"index":0,"delta":{"content":"oi"}}]}`,
		`{"id":"gen-u","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`,
	})
	defer srv.Close()

	svc := newTestService(srv.URL, &capturingPub{})
	ch, _ := svc.GenerateStream(context.Background(), ai.GenerateInput{
		Model: "x-ai/grok-4.7", WorkspaceID: "ws-1",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})
	done, failure := streamOutcome(ch)

	if failure != nil || done == nil {
		t.Fatalf("done = %+v, error = %v; a stream with usage finished", done, failure)
	}
}
