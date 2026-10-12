package copilot_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/ai"
	"vozko/domain/copilot"
	"vozko/usecases/agentloop"
)

var droppedConnection = fmt.Errorf("%w: stream: read tcp: wsarecv: the connected host did not respond", ai.ErrProviderTransient)

type eventLog struct {
	mu     sync.Mutex
	errors []string
	codes  []string
	types  []string
}

func (l *eventLog) emit(eventType string, payload interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.types = append(l.types, eventType)
	if eventType != "error" {
		return
	}
	if p, ok := payload.(map[string]interface{}); ok {
		if text, ok := p["error"].(string); ok {
			l.errors = append(l.errors, text)
		}
		if code, ok := p["code"].(string); ok {
			l.codes = append(l.codes, code)
		}
	}
}

func quickService(prov *scriptAI, th *fakeThreads, ms *fakeMessages, ts ...copilot.Tool) *Service {
	svc := newService(prov, th, ms, ts...)
	svc.retryBase = time.Nanosecond
	return svc
}

func TestAConnectionThatKeepsDroppingTellsTheUserHonestlyAndKeepsTheWorkDone(t *testing.T) {
	th := &fakeThreads{thread: testThread()}
	ms := &fakeMessages{}
	rt := &fakeTool{name: "list_agents", meta: readMeta}
	prov := &scriptAI{
		turns:  [][]ai.ToolCall{{call("list_agents", nil)}},
		faults: []error{nil, droppedConnection, droppedConnection, droppedConnection, droppedConnection, droppedConnection},
	}
	log := &eventLog{}
	if err := quickService(prov, th, ms, rt).Stream(context.Background(), th.thread, copilot.UserMessage{Content: "quantos agentes?"}, ownerCtx, log.emit); err != nil {
		t.Fatalf("stream: %v", err)
	}

	if rt.calls != 1 {
		t.Fatalf("the tool that already ran must run once, ran %d times", rt.calls)
	}
	if len(log.errors) != 1 || log.errors[0] != msgProviderUnreachable || len(log.codes) != 1 || log.codes[0] != ErrorCodeProviderUnreachable {
		t.Fatalf("the person must be told the connection dropped, got %v %v", log.errors, log.codes)
	}
	saved := ms.last()
	var steps []toolStep
	if saved == nil || json.Unmarshal(saved.ToolCalls, &steps) != nil || len(steps) != 1 || steps[0].Name != "list_agents" {
		t.Fatalf("the completed work must stay in the conversation, got %+v", saved)
	}
}

func TestAPassingDropIsRetriedAndTheReplyKeepsNoTraceOfTheFailedAttempt(t *testing.T) {
	th := &fakeThreads{thread: testThread()}
	ms := &fakeMessages{}
	prov := &scriptAI{
		texts:  []string{"", "Tudo certo."},
		faults: []error{droppedConnection},
		drafts: []string{"rascunho perdido ", "pensando de novo"},
	}
	log := &eventLog{}
	if err := quickService(prov, th, ms).Stream(context.Background(), th.thread, copilot.UserMessage{Content: "oi"}, ownerCtx, log.emit); err != nil {
		t.Fatalf("stream: %v", err)
	}

	if len(log.errors) != 0 {
		t.Fatalf("a recovered drop is not an error, got %v", log.errors)
	}
	saved := ms.last()
	if saved == nil || saved.Content != "Tudo certo." {
		t.Fatalf("reply = %+v", saved)
	}
	var reasoning string
	if json.Unmarshal(saved.Reasoning, &reasoning) != nil || reasoning != "pensando de novo" {
		t.Fatalf("the saved reasoning must drop what the failed attempt showed, got %q", reasoning)
	}
}

func TestAStoppedTurnIsNeverReportedAsAConnectionFailure(t *testing.T) {
	th := &fakeThreads{thread: testThread()}
	ms := &fakeMessages{}
	prov := &scriptAI{faults: []error{droppedConnection, droppedConnection}}
	svc := newService(prov, th, ms)
	svc.retryBase = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	log := &eventLog{}
	emit := func(eventType string, payload interface{}) {
		log.emit(eventType, payload)
		if eventType == agentloop.EventAssistantRetry {
			cancel()
		}
	}

	finished := make(chan error, 1)
	go func() {
		finished <- svc.Stream(ctx, th.thread, copilot.UserMessage{Content: "oi"}, ownerCtx, emit)
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the backoff outlived the stopped turn")
	}
	if len(log.errors) != 0 {
		t.Fatalf("a stopped turn must not read as a provider failure, got %v", log.errors)
	}
}

func TestHaltMessagesTellAnUnreachableProviderFromAnInternalFailure(t *testing.T) {
	unreachable := fmt.Errorf("%w: %w: %w", agentloop.ErrProviderFailed, agentloop.ErrProviderUnreachable, droppedConnection)
	if got := haltMessage(unreachable); got != msgProviderUnreachable || haltCode(unreachable) != ErrorCodeProviderUnreachable {
		t.Fatalf("haltMessage = %q, code = %q", got, haltCode(unreachable))
	}
	if strings.Contains(strings.ToLower(msgProviderUnreachable), "interna") {
		t.Fatal("a dropped connection is not an internal failure")
	}
	refused := fmt.Errorf("%w: %w", agentloop.ErrProviderFailed, errors.New("status 402"))
	if got := haltMessage(refused); got != msgProviderFailed || haltCode(refused) != "" {
		t.Fatalf("a refusal keeps the internal failure message, got %q", got)
	}
}

func TestTheRecorderDropsTheReasoningOfAFailedAttempt(t *testing.T) {
	rec := &turnRecorder{emit: func(string, interface{}) {}}
	rec.emitFn("reasoning_delta", map[string]string{"text": "plano firme. "})
	rec.emitFn("reasoning_delta", map[string]string{"text": "rascunho"})
	rec.emitFn(agentloop.EventAssistantRetry, agentloop.RetryNotice{Attempt: 1, Of: 3, Reasoning: "rascunho"})
	rec.emitFn("reasoning_delta", map[string]string{"text": "plano novo"})
	var reasoning string
	m, err := rec.message("t1", "x", "m", nil)
	if err != nil || json.Unmarshal(m.Reasoning, &reasoning) != nil || reasoning != "plano firme. plano novo" {
		t.Fatalf("reasoning = %q", reasoning)
	}
}
