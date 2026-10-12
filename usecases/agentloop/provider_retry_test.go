package agentloop

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"vozko/domain/ai"
)

var connectionDropped = fmt.Errorf("%w: stream: read tcp: wsarecv: connection timed out", ai.ErrProviderTransient)

func quickRetries(cfg Config) Config {
	cfg.ProviderRetryBase = time.Nanosecond
	return cfg
}

type payloadLog struct {
	mu      sync.Mutex
	types   []string
	retries []RetryNotice
	onRetry func()
}

func (l *payloadLog) emit(eventType string, payload interface{}) {
	l.mu.Lock()
	l.types = append(l.types, eventType)
	if notice, ok := payload.(RetryNotice); ok && eventType == EventAssistantRetry {
		l.retries = append(l.retries, notice)
	}
	hook := l.onRetry
	l.mu.Unlock()
	if eventType == EventAssistantRetry && hook != nil {
		hook()
	}
}

func TestATransientFailureRetriesTheStepFromTheLastToolResultsWithoutRerunningTools(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{tcs: []ai.ToolCall{{ID: "c1", Name: "generate"}}},
		{errEvent: connectionDropped},
		{tokens: []string{"Pronto."}},
	}}
	drv := &fakeDriver{}
	out, _, _ := run(t, prov, drv, quickRetries(Config{}))

	if out.Kind != OutcomeIdle || out.Halt != nil {
		t.Fatalf("a passing fault must not end the turn, got %+v", out)
	}
	if !reflect.DeepEqual(drv.dispatched, []string{"generate"}) {
		t.Fatalf("tools that already ran must never run again, dispatched %v", drv.dispatched)
	}
	if len(prov.inputs) != 3 {
		t.Fatalf("want the first step, the failed step and its retry, got %d calls", len(prov.inputs))
	}
	if !reflect.DeepEqual(prov.inputs[1].Messages, prov.inputs[2].Messages) {
		t.Fatal("the retry must resend exactly the failed request, carrying the tool results already obtained")
	}
	last := prov.inputs[2].Messages
	var sawResult bool
	for _, m := range last {
		if m.Role == ai.RoleTool && m.ToolCallID == "c1" {
			sawResult = true
		}
	}
	if !sawResult {
		t.Fatal("the retried request must carry the result of the tool that already ran")
	}
}

func TestAFailureBeforeTheStreamOpensIsRetriedToo(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{openErr: connectionDropped}, {tokens: []string{"Oi."}}}}
	out, _, _ := run(t, prov, &fakeDriver{}, quickRetries(Config{}))
	if out.Kind != OutcomeIdle || len(prov.inputs) != 2 {
		t.Fatalf("got %+v after %d calls", out, len(prov.inputs))
	}
}

func TestRetriesAreBoundedAndThenHaltAsAnUnreachableProvider(t *testing.T) {
	turns := make([]aiTurn, 10)
	for i := range turns {
		turns[i] = aiTurn{openErr: connectionDropped}
	}
	prov := &fakeAI{turns: turns}
	out, _, _ := run(t, prov, &fakeDriver{}, quickRetries(Config{ProviderRetries: 2}))

	if len(prov.inputs) != 3 {
		t.Fatalf("two retries mean three attempts, got %d", len(prov.inputs))
	}
	if !errors.Is(out.Halt, ErrProviderFailed) || !errors.Is(out.Halt, ErrProviderUnreachable) || out.Valid {
		t.Fatalf("exhausted retries must halt as an unreachable provider, got %+v", out)
	}
}

func TestAPermanentProviderRefusalIsNeverRetried(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{openErr: errors.New("status 402: insufficient credits")}, {tokens: []string{"Oi."}}}}
	out, _, _ := run(t, prov, &fakeDriver{}, quickRetries(Config{}))
	if len(prov.inputs) != 1 || !errors.Is(out.Halt, ErrProviderFailed) || errors.Is(out.Halt, ErrProviderUnreachable) {
		t.Fatalf("a refusal must halt at once, got %+v after %d calls", out, len(prov.inputs))
	}
}

func TestARetryTakesBackWhatTheFailedAttemptAlreadyShowed(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{reasoning: []string{"pensando no layout"}, tokens: []string{"Vou montar "}, errEvent: connectionDropped},
		{tokens: []string{"Vou montar a arte."}},
	}}
	log := &payloadLog{}
	out := (&Engine{AI: prov}).Run(context.Background(), log.emit, &fakeDriver{}, quickRetries(Config{}), &Session{}, "x")

	if out.Kind != OutcomeIdle {
		t.Fatalf("got %+v", out)
	}
	if len(log.retries) != 1 {
		t.Fatalf("want one retry notice, got %+v", log.retries)
	}
	notice := log.retries[0]
	if notice.Text != "Vou montar " || notice.Reasoning != "pensando no layout" || notice.Attempt != 1 || notice.Of != 3 {
		t.Fatalf("the notice must name exactly what to take back, got %+v", notice)
	}
}

func TestTheGraceAnswerAlsoSurvivesAPassingFault(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{tcs: []ai.ToolCall{tcall("look")}},
		{openErr: connectionDropped},
		{tokens: []string{"Resumo do que fiz."}},
	}}
	out, _, sess := run(t, prov, &fakeDriver{}, quickRetries(Config{MaxIterations: 1, GraceInstruction: "responda"}))
	if out.Kind != OutcomeIdle || lastAssistant(sess.History) != "Resumo do que fiz." {
		t.Fatalf("got %+v", out)
	}
}

func lastAssistant(h []ai.Message) string {
	for i := len(h) - 1; i >= 0; i-- {
		if h[i].Role == ai.RoleAssistant {
			return h[i].Content
		}
	}
	return ""
}

func TestACancelledTurnDuringTheBackoffEndsAtOnceAsCancelledNotAsAProviderFailure(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{openErr: connectionDropped}, {tokens: []string{"Oi."}}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := &payloadLog{onRetry: cancel}
	cfg := Config{ProviderRetryBase: time.Hour}

	done := make(chan Outcome, 1)
	go func() { done <- (&Engine{AI: prov}).Run(ctx, log.emit, &fakeDriver{}, cfg, &Session{}, "x") }()
	select {
	case out := <-done:
		if out.Summary != reasonCancelled || out.Halt != nil {
			t.Fatalf("a stopped turn must read as cancelled, got %+v", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the backoff outlived the cancelled turn")
	}
	if len(prov.inputs) != 1 {
		t.Fatalf("no call may start after the turn was cancelled, got %d", len(prov.inputs))
	}
}

func TestACancelledTurnDuringTheRetriedCallEndsAsCancelledNotAsAProviderFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prov := &fakeAI{
		turns: []aiTurn{{openErr: connectionDropped}, {block: true}, {tokens: []string{"Oi."}}},
		onCall: func(n int) {
			if n == 2 {
				go func() {
					time.Sleep(10 * time.Millisecond)
					cancel()
				}()
			}
		},
	}

	done := make(chan Outcome, 1)
	go func() {
		done <- (&Engine{AI: prov}).Run(ctx, (&capture{}).emit, &fakeDriver{}, quickRetries(Config{}), &Session{}, "x")
	}()
	select {
	case out := <-done:
		if out.Summary != reasonCancelled || out.Halt != nil {
			t.Fatalf("a stopped turn must read as cancelled, got %+v", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the retried call outlived the cancelled turn")
	}
	if len(prov.inputs) != 2 {
		t.Fatalf("the cancelled retry must not be retried again, got %d calls", len(prov.inputs))
	}
}

func TestTheBackoffGrowsAndStaysNearItsStep(t *testing.T) {
	cfg := Config{ProviderRetryBase: time.Second}
	prev := time.Duration(0)
	for attempt := 0; attempt < 3; attempt++ {
		d := cfg.retryDelay(attempt)
		floor := time.Second
		for i := 0; i < attempt; i++ {
			floor *= retryGrowth
		}
		if d < floor || d > floor+floor/4 || d <= prev {
			t.Fatalf("attempt %d waited %s, want between %s and %s and longer than %s", attempt, d, floor, floor+floor/4, prev)
		}
		prev = d
	}
}
