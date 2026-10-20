package openrouter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/ai"
)

const callerReleaseLimit = 300 * time.Millisecond

type leakTally struct {
	mu      sync.Mutex
	reasons []string
}

func (l *leakTally) IncBillingSkipped(reason string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reasons = append(l.reasons, reason)
}

func (l *leakTally) seen() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.reasons...)
}

type usageByID struct {
	mu     sync.Mutex
	usage  map[string][2]int
	asked  []string
	broken bool
}

func (f *usageByID) FetchUsage(_ context.Context, id string) (int, int, int64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, id)
	if f.broken {
		return 0, 0, 0, false
	}
	u, ok := f.usage[id]
	return u[0], u[1], 0, ok
}

func (f *usageByID) askedFor() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

type providerScript struct {
	srv          *httptest.Server
	disconnected chan struct{}
	requests     atomic.Int32
}

func scriptedProvider(t *testing.T, headerDelay time.Duration, headerID string, script func(n int32, send func(string), r *http.Request)) *providerScript {
	t.Helper()
	p := &providerScript{disconnected: make(chan struct{}, 8)}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := p.requests.Add(1)
		defer func() { p.disconnected <- struct{}{} }()
		select {
		case <-time.After(headerDelay):
		case <-r.Context().Done():
			return
		}
		if headerID != "" {
			w.Header().Set("X-Generation-Id", headerID)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		flusher.Flush()
		send := func(chunk string) {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}
		script(n, send, r)
		<-r.Context().Done()
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *providerScript) awaitDisconnect(t *testing.T, within time.Duration) {
	t.Helper()
	select {
	case <-p.disconnected:
	case <-time.After(within):
		t.Fatalf("the provider stream was still open after %v", within)
	}
}

func cancellableService(url string, pub *capturingPub, fetcher generationUsageFetcher, leaks *leakTally) *Service {
	svc := newTestService(url, pub)
	svc.usageFetcher = fetcher
	svc.usageRetryDelays = []time.Duration{0}
	svc.streamDrainTimeout = 5 * time.Second
	svc.billingMetrics = leaks
	return svc
}

func cancellableInput() ai.GenerateInput {
	return ai.GenerateInput{
		Model: "anthropic/claude-sonnet-5.5", WorkspaceID: "ws-1", BillingReference: "workflow:builder-1",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "monte o fluxo"}},
	}
}

func releasedWithin(t *testing.T, ch <-chan ai.StreamEvent, limit time.Duration) error {
	t.Helper()
	var failure error
	deadline := time.After(limit)
	for {
		select {
		case ev, open := <-ch:
			if !open {
				return failure
			}
			if ev.Type == ai.StreamEventError {
				failure = ev.Error
			}
		case <-deadline:
			t.Fatalf("the caller was still held %v after cancelling", limit)
		}
	}
}

func settledWithin(t *testing.T, svc *Service, limit time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		svc.recoveries.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("billing work was still running %v later", limit)
	}
}

func singleCharge(t *testing.T, pub *capturingPub, prompt, completion int) {
	t.Helper()
	events := pub.events()
	if len(events) != 1 || events[0].PromptTokens != prompt || events[0].CompletionTokens != completion || events[0].WorkspaceID != "ws-1" {
		t.Fatalf("billing = %+v, want one charge of %d/%d on ws-1", events, prompt, completion)
	}
}

func TestCancellingAfterChunksReleasesTheCallerAndRecoversByIDWithoutDraining(t *testing.T) {
	provider := scriptedProvider(t, 0, "", func(_ int32, send func(string), _ *http.Request) {
		send(`{"id":"gen-after","choices":[{"index":0,"delta":{"content":"Vou montar"}}]}`)
	})
	pub := &capturingPub{}
	fetcher := &usageByID{usage: map[string][2]int{"gen-after": {900, 40}}}
	svc := cancellableService(provider.srv.URL, pub, fetcher, &leakTally{})
	svc.streamDrainTimeout = time.Minute

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := svc.GenerateStream(ctx, cancellableInput())
	if err != nil {
		t.Fatal(err)
	}
	for ev := range ch {
		if ev.Type == ai.StreamEventToken {
			break
		}
	}
	cancel()

	if failure := releasedWithin(t, ch, callerReleaseLimit); !errors.Is(failure, context.Canceled) {
		t.Fatalf("caller error = %v, want context.Canceled", failure)
	}
	provider.awaitDisconnect(t, time.Second)
	settledWithin(t, svc, 2*time.Second)
	singleCharge(t, pub, 900, 40)
}

func TestCancellingBeforeTheFirstChunkDrainsUntilTheIDArrivesThenRecoversTheCost(t *testing.T) {
	idSent := make(chan time.Time, 1)
	provider := scriptedProvider(t, 0, "", func(_ int32, send func(string), r *http.Request) {
		select {
		case <-time.After(time.Second):
			send(`{"id":"gen-late","choices":[{"index":0,"delta":{"reasoning":"pensando"}}]}`)
			idSent <- time.Now()
		case <-r.Context().Done():
		}
	})
	pub := &capturingPub{}
	fetcher := &usageByID{usage: map[string][2]int{"gen-late": {48000, 0}}}
	leaks := &leakTally{}
	svc := cancellableService(provider.srv.URL, pub, fetcher, leaks)

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := svc.GenerateStream(ctx, cancellableInput())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	cancel()

	if failure := releasedWithin(t, ch, callerReleaseLimit); !errors.Is(failure, context.Canceled) {
		t.Fatalf("caller error = %v, want context.Canceled", failure)
	}
	provider.awaitDisconnect(t, 3*time.Second)
	select {
	case <-idSent:
	default:
		t.Fatal("the drain closed the provider stream before the generation id arrived")
	}
	settledWithin(t, svc, 2*time.Second)

	singleCharge(t, pub, 48000, 0)
	if asked := fetcher.askedFor(); len(asked) != 1 || asked[0] != "gen-late" {
		t.Fatalf("recovery asked for %v, want [gen-late]", asked)
	}
	if got := leaks.seen(); len(got) != 0 {
		t.Fatalf("a recovered generation is no leak, got %v", got)
	}
}

func TestCancellingBeforeTheFirstChunkWithNoIDWithinTheBoundClosesTheStreamAndCountsTheLeak(t *testing.T) {
	provider := scriptedProvider(t, 0, "", func(int32, func(string), *http.Request) {})
	pub := &capturingPub{}
	leaks := &leakTally{}
	svc := cancellableService(provider.srv.URL, pub, &usageByID{}, leaks)
	svc.streamDrainTimeout = 300 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := svc.GenerateStream(ctx, cancellableInput())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	cancel()

	releasedWithin(t, ch, callerReleaseLimit)
	provider.awaitDisconnect(t, 2*time.Second)
	settledWithin(t, svc, time.Second)

	if got := len(pub.events()); got != 0 {
		t.Fatalf("nothing known means nothing billed, got %d events", got)
	}
	if got := leaks.seen(); len(got) != 1 || got[0] != "no_id_before_timeout" {
		t.Fatalf("leak reasons = %v, want [no_id_before_timeout]", got)
	}
}

func TestTheHeaderIDLetsACancelBeforeAnyChunkRecoverWithoutWaitingForChunks(t *testing.T) {
	provider := scriptedProvider(t, 200*time.Millisecond, "gen-header", func(int32, func(string), *http.Request) {})
	pub := &capturingPub{}
	fetcher := &usageByID{usage: map[string][2]int{"gen-header": {30000, 0}}}
	svc := cancellableService(provider.srv.URL, pub, fetcher, &leakTally{})
	svc.streamDrainTimeout = time.Minute

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := svc.GenerateStream(ctx, cancellableInput())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	cancel()

	releasedWithin(t, ch, callerReleaseLimit)
	provider.awaitDisconnect(t, 2*time.Second)
	settledWithin(t, svc, 2*time.Second)
	singleCharge(t, pub, 30000, 0)
}

func TestAProviderAbortAfterTheIDRecoversByID(t *testing.T) {
	provider := scriptedProvider(t, 0, "", func(_ int32, send func(string), _ *http.Request) {
		send(`{"id":"gen-abort","choices":[{"index":0,"delta":{"content":"Vou"}}]}`)
		panic(http.ErrAbortHandler)
	})
	pub := &capturingPub{}
	fetcher := &usageByID{usage: map[string][2]int{"gen-abort": {500, 3}}}
	svc := cancellableService(provider.srv.URL, pub, fetcher, &leakTally{})

	ch, err := svc.GenerateStream(context.Background(), cancellableInput())
	if err != nil {
		t.Fatal(err)
	}
	if failure := releasedWithin(t, ch, 2*time.Second); !errors.Is(failure, ai.ErrStreamIncomplete) {
		t.Fatalf("error = %v, want an incomplete stream", failure)
	}
	settledWithin(t, svc, 2*time.Second)
	singleCharge(t, pub, 500, 3)
}

func TestAProviderAbortBeforeAnyChunkCountsAProviderErrorLeak(t *testing.T) {
	provider := scriptedProvider(t, 0, "", func(int32, func(string), *http.Request) {
		panic(http.ErrAbortHandler)
	})
	pub := &capturingPub{}
	leaks := &leakTally{}
	svc := cancellableService(provider.srv.URL, pub, &usageByID{}, leaks)

	ch, err := svc.GenerateStream(context.Background(), cancellableInput())
	if err != nil {
		t.Fatal(err)
	}
	releasedWithin(t, ch, 2*time.Second)
	settledWithin(t, svc, time.Second)

	if got := len(pub.events()); got != 0 {
		t.Fatalf("no id and no usage bills nothing, got %d events", got)
	}
	if got := leaks.seen(); len(got) != 1 || got[0] != "provider_error" {
		t.Fatalf("leak reasons = %v, want [provider_error]", got)
	}
}

func TestARecoveryThatKeepsFailingCountsARecoveryFailedLeak(t *testing.T) {
	provider := scriptedProvider(t, 0, "", func(_ int32, send func(string), _ *http.Request) {
		send(`{"id":"gen-lost","choices":[{"index":0,"delta":{"content":"Vou"}}]}`)
	})
	pub := &capturingPub{}
	leaks := &leakTally{}
	svc := cancellableService(provider.srv.URL, pub, &usageByID{broken: true}, leaks)
	svc.usageRetryDelays = []time.Duration{0, time.Millisecond, time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := svc.GenerateStream(ctx, cancellableInput())
	if err != nil {
		t.Fatal(err)
	}
	for ev := range ch {
		if ev.Type == ai.StreamEventToken {
			break
		}
	}
	cancel()
	releasedWithin(t, ch, callerReleaseLimit)
	settledWithin(t, svc, 2*time.Second)

	if got := len(pub.events()); got != 0 {
		t.Fatalf("an unrecovered cost is never estimated, got %d events", got)
	}
	if got := leaks.seen(); len(got) != 1 || got[0] != "recovery_failed" {
		t.Fatalf("leak reasons = %v, want [recovery_failed]", got)
	}
}

func TestTwoStreamsCancelledTogetherEachBillTheirOwnGeneration(t *testing.T) {
	provider := scriptedProvider(t, 0, "", func(n int32, send func(string), r *http.Request) {
		select {
		case <-time.After(300 * time.Millisecond):
			send(fmt.Sprintf(`{"id":"gen-%d","choices":[{"index":0,"delta":{"content":"oi"}}]}`, n))
		case <-r.Context().Done():
		}
	})
	pub := &capturingPub{}
	fetcher := &usageByID{usage: map[string][2]int{"gen-1": {100, 0}, "gen-2": {200, 0}}}
	svc := cancellableService(provider.srv.URL, pub, fetcher, &leakTally{})

	ctx, cancel := context.WithCancel(context.Background())
	var streams []<-chan ai.StreamEvent
	for range 2 {
		ch, err := svc.GenerateStream(ctx, cancellableInput())
		if err != nil {
			t.Fatal(err)
		}
		streams = append(streams, ch)
	}
	time.Sleep(100 * time.Millisecond)
	cancel()
	for _, ch := range streams {
		releasedWithin(t, ch, callerReleaseLimit)
	}
	provider.awaitDisconnect(t, 2*time.Second)
	provider.awaitDisconnect(t, 2*time.Second)
	settledWithin(t, svc, 2*time.Second)

	billed := map[int]bool{}
	for _, e := range pub.events() {
		billed[e.PromptTokens] = true
	}
	if len(pub.events()) != 2 || !billed[100] || !billed[200] {
		t.Fatalf("billing = %+v, want one charge per generation (100 and 200)", pub.events())
	}
}

func TestACallerThatStopsReadingNeverBlocksTheDrain(t *testing.T) {
	provider := scriptedProvider(t, 0, "", func(_ int32, send func(string), r *http.Request) {
		select {
		case <-time.After(200 * time.Millisecond):
			for i := range 100 {
				send(fmt.Sprintf(`{"id":"gen-flood","choices":[{"index":0,"delta":{"content":"t%d"}}]}`, i))
			}
		case <-r.Context().Done():
		}
	})
	pub := &capturingPub{}
	fetcher := &usageByID{usage: map[string][2]int{"gen-flood": {70, 70}}}
	svc := cancellableService(provider.srv.URL, pub, fetcher, &leakTally{})

	ctx, cancel := context.WithCancel(context.Background())
	if _, err := svc.GenerateStream(ctx, cancellableInput()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	cancel()

	provider.awaitDisconnect(t, 2*time.Second)
	settledWithin(t, svc, 2*time.Second)
	singleCharge(t, pub, 70, 70)
}

func completionProvider(t *testing.T, headerID string, delay time.Duration, body string) *providerScript {
	t.Helper()
	p := &providerScript{disconnected: make(chan struct{}, 8)}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { p.disconnected <- struct{}{} }()
		if headerID != "" {
			w.Header().Set("X-Generation-Id", headerID)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-time.After(delay):
			fmt.Fprint(w, body)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(p.srv.Close)
	return p
}

const completionBody = `{"id":"gen-blocking","choices":[{"index":0,"message":{"role":"assistant","content":"pronto"},"finish_reason":"stop"}],"usage":{"prompt_tokens":640,"completion_tokens":12,"total_tokens":652,"cost":0.002}}`

func TestACancelledCompletionReleasesTheCallerAndBillsTheResponseThatStillArrives(t *testing.T) {
	provider := completionProvider(t, "gen-blocking", 400*time.Millisecond, completionBody)
	pub := &capturingPub{}
	fetcher := &usageByID{}
	svc := cancellableService(provider.srv.URL, pub, fetcher, &leakTally{})

	ctx, cancel := context.WithCancel(context.Background())
	failure := make(chan error, 1)
	go func() {
		_, err := svc.Generate(ctx, cancellableInput())
		failure <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-failure:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(callerReleaseLimit):
		t.Fatal("the caller was held after cancelling a completion")
	}
	settledWithin(t, svc, 2*time.Second)
	singleCharge(t, pub, 640, 12)
	if asked := fetcher.askedFor(); len(asked) != 0 {
		t.Fatalf("the response carried its usage, recovery must not run: %v", asked)
	}
}

func TestACancelledCompletionPastItsBoundRecoversByTheHeaderID(t *testing.T) {
	provider := completionProvider(t, "gen-slow", time.Minute, completionBody)
	pub := &capturingPub{}
	fetcher := &usageByID{usage: map[string][2]int{"gen-slow": {7000, 900}}}
	svc := cancellableService(provider.srv.URL, pub, fetcher, &leakTally{})
	svc.completionWait = 300 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _, _ = svc.Generate(ctx, cancellableInput()) }()
	time.Sleep(50 * time.Millisecond)
	cancel()

	provider.awaitDisconnect(t, 2*time.Second)
	settledWithin(t, svc, 2*time.Second)
	singleCharge(t, pub, 7000, 900)
}

func TestACompletionWithoutUsageIsRecoveredByItsID(t *testing.T) {
	provider := completionProvider(t, "", 0, `{"id":"gen-bare","choices":[{"index":0,"message":{"role":"assistant","content":"pronto"},"finish_reason":"stop"}]}`)
	pub := &capturingPub{}
	fetcher := &usageByID{usage: map[string][2]int{"gen-bare": {11, 2}}}
	svc := cancellableService(provider.srv.URL, pub, fetcher, &leakTally{})

	if _, err := svc.Generate(context.Background(), cancellableInput()); err != nil {
		t.Fatal(err)
	}
	settledWithin(t, svc, 2*time.Second)
	singleCharge(t, pub, 11, 2)
}
