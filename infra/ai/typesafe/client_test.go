package typesafe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/decision"
)

type charge struct {
	workspaceID string
	model       string
	prompt      int
	completion  int
	costMicros  int64
}

type recordingBilling struct {
	mu      sync.Mutex
	charges []charge
	done    chan struct{}
}

func newRecordingBilling() *recordingBilling {
	return &recordingBilling{done: make(chan struct{}, 8)}
}

func (b *recordingBilling) Publish(workspaceID, model string, prompt, completion int, costMicros int64) {
	b.mu.Lock()
	b.charges = append(b.charges, charge{workspaceID, model, prompt, completion, costMicros})
	b.mu.Unlock()
	b.done <- struct{}{}
}

func (b *recordingBilling) waitFor(t *testing.T, n int) []charge {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-b.done:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d charges arrived", i, n)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]charge(nil), b.charges...)
}

func (b *recordingBilling) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.charges)
}

func request() decision.Request {
	return decision.Request{
		WorkspaceID: "ws-1",
		State:       map[string]any{"conversa": []string{"cliente: fechado, manda o link"}},
		Questions: map[string]decision.Question{
			"stage": decision.Choice("Qual etapa?",
				decision.Option{Key: "negociacao", Description: "Discute preço"},
				decision.Option{Key: "fechamento", Description: "Confirmou a compra"},
			),
			"handoff": decision.YesNo("Pediu humano?", "Pediu", "Não pediu"),
			"quality": decision.Score("Qualidade", "baixa", "média", "alta"),
		},
	}
}

const goodAnswer = `{
  "model": "typesafe/jev-1.13-20260917",
  "answers": {
    "stage": {"type": "choice", "choice": "fechamento", "probabilities": {"negociacao": 0.05, "fechamento": 0.95}, "confidence": 0.93},
    "handoff": {"type": "noul", "noul": 0.02},
    "quality": {"type": "score", "score": 1.6, "confidence": 0.71}
  },
  "usage": {"input_tokens": 514, "output_tokens": 78, "cost": 0.000021588}
}`

func newClient(t *testing.T, handler http.HandlerFunc, billing Billing) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New(Config{APIKey: "key", BaseURL: server.URL, Model: "typesafe/jev-1.13"}, billing)
	if err != nil {
		t.Fatal(err)
	}
	client.retryDelays = []time.Duration{time.Millisecond, time.Millisecond}
	return client
}

func TestTheRequestCarriesStateAndTypedQuestions(t *testing.T) {
	var body map[string]any
	var auth, path string
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = io.WriteString(w, goodAnswer)
	}, newRecordingBilling())

	if _, err := client.Decide(context.Background(), request()); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer key" || path != "/decisions" {
		t.Fatalf("auth=%q path=%q", auth, path)
	}
	if body["model"] != "typesafe/jev-1.13" || body["state"] == nil {
		t.Fatalf("body = %v", body)
	}
	questions := body["questions"].(map[string]any)
	stage := questions["stage"].(map[string]any)
	if stage["type"] != "choice" || stage["criteria"].(map[string]any)["fechamento"] != "Confirmou a compra" {
		t.Fatalf("stage question = %v", stage)
	}
	handoff := questions["handoff"].(map[string]any)["criteria"].(map[string]any)
	if handoff["true"] != "Pediu" || handoff["false"] != "Não pediu" {
		t.Fatalf("yes/no criteria = %v", handoff)
	}
	levels := questions["quality"].(map[string]any)["criteria"].([]any)
	if len(levels) != 3 || levels[2] != "alta" {
		t.Fatalf("score levels = %v", levels)
	}
}

func TestAnswersComeBackTypedAndTheCallIsBilled(t *testing.T) {
	billing := newRecordingBilling()
	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, goodAnswer) }, billing)

	result, err := client.Decide(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	stage, _ := result.Answer("stage")
	if key, ok := stage.Chosen(0.9); !ok || key != "fechamento" || stage.Probabilities["fechamento"] != 0.95 {
		t.Fatalf("stage = %+v", stage)
	}
	handoff, _ := result.Answer("handoff")
	if !handoff.Denied(0.9) {
		t.Fatalf("handoff = %+v", handoff)
	}
	quality, _ := result.Answer("quality")
	if quality.Score != 1.6 || quality.Confidence != 0.71 {
		t.Fatalf("quality = %+v", quality)
	}
	if result.InputTokens != 514 || result.CostMicros != 22 || result.Model != "typesafe/jev-1.13" {
		t.Fatalf("result usage = %+v", result)
	}
	charges := billing.waitFor(t, 1)
	if charges[0] != (charge{"ws-1", "typesafe/jev-1.13", 514, 0, 22}) {
		t.Fatalf("charge = %+v", charges[0])
	}
}

func TestBillingNeverDelaysTheDecision(t *testing.T) {
	slow := &blockingBilling{release: make(chan struct{})}
	defer close(slow.release)
	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, goodAnswer) }, slow)

	finished := make(chan error, 1)
	go func() {
		_, err := client.Decide(context.Background(), request())
		finished <- err
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("the decision waited for the billing queue")
	}
}

type blockingBilling struct{ release chan struct{} }

func (b *blockingBilling) Publish(string, string, int, int, int64) { <-b.release }

func TestOverloadIsRetriedThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(529)
			return
		}
		_, _ = io.WriteString(w, goodAnswer)
	}, newRecordingBilling())

	if _, err := client.Decide(context.Background(), request()); err != nil {
		t.Fatalf("Decide() = %v after retries", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestPersistentOverloadIsUnavailable(t *testing.T) {
	var calls atomic.Int32
	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}, newRecordingBilling())

	if _, err := client.Decide(context.Background(), request()); !errors.Is(err, decision.ErrUnavailable) {
		t.Fatalf("Decide() = %v, want ErrUnavailable", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 1 attempt plus 2 retries", calls.Load())
	}
}

func TestAClientErrorIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnprocessableEntity)
	}, newRecordingBilling())

	if _, err := client.Decide(context.Background(), request()); !errors.Is(err, decision.ErrUnavailable) {
		t.Fatalf("Decide() = %v, want ErrUnavailable", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestAnAnswerOutsideTheQuestionIsRejectedButStillBilled(t *testing.T) {
	billing := newRecordingBilling()
	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"answers":{"stage":{"type":"choice","choice":"inventada","confidence":1},"handoff":{"type":"noul","noul":0.1},"quality":{"type":"score","score":1,"confidence":1}},"usage":{"input_tokens":400,"cost":0.00002}}`)
	}, billing)

	if _, err := client.Decide(context.Background(), request()); !errors.Is(err, decision.ErrInvalidAnswer) {
		t.Fatalf("Decide() = %v, want ErrInvalidAnswer", err)
	}
	if billing.waitFor(t, 1)[0].costMicros != 20 {
		t.Fatal("a call that was paid for must be billed even when its answer is unusable")
	}
}

func TestAnInvalidRequestNeverReachesTheProvider(t *testing.T) {
	var calls atomic.Int32
	billing := newRecordingBilling()
	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1) }, billing)

	invalid := request()
	invalid.WorkspaceID = ""
	if _, err := client.Decide(context.Background(), invalid); !errors.Is(err, decision.ErrInvalidRequest) {
		t.Fatalf("Decide() = %v, want ErrInvalidRequest", err)
	}
	if calls.Load() != 0 || billing.count() != 0 {
		t.Fatal("an invalid request must not be sent or billed")
	}
}

func TestACancelledCallerStopsRetrying(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(529) }, newRecordingBilling())
	client.retryDelays = []time.Duration{time.Hour}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	started := time.Now()
	if _, err := client.Decide(ctx, request()); !errors.Is(err, decision.ErrUnavailable) {
		t.Fatalf("Decide() = %v, want ErrUnavailable", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("the retry wait ignored the caller's deadline")
	}
}

func TestTheClientRefusesToRunWithoutBilling(t *testing.T) {
	if _, err := New(Config{APIKey: "key", Model: "m"}, nil); err == nil {
		t.Fatal("a decision client without billing would give decisions away for free")
	}
	if _, err := New(Config{Model: "m"}, newRecordingBilling()); err == nil {
		t.Fatal("a decision client needs an API key")
	}
}
