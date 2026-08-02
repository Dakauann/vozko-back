package agentloop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/ai"
	"vozko/domain/tools"
)

// ---- fakes ---------------------------------------------------------------

// aiTurn scripts one model turn for the fake provider.
type aiTurn struct {
	reasoning []string // StreamEventReasoning tokens
	tokens    []string // StreamEventToken tokens
	tcs       []ai.ToolCall
	fullText  string
	finish    string
	usage     *ai.Usage
	noUsage   bool  // omit Usage on the done event
	errEvent  error // emit a StreamEventError instead of done
}

type fakeAI struct {
	turns    []aiTurn
	errAt    int // 1-based turn index where GenerateStream returns an error (0 = never)
	blockCtx bool
	delay    time.Duration
	mu       sync.Mutex
	idx      int
	models   []string
}

func (f *fakeAI) Generate(ctx context.Context, in ai.GenerateInput) (*ai.GenerateOutput, error) {
	return nil, fmt.Errorf("unused")
}

func (f *fakeAI) GenerateStream(ctx context.Context, in ai.GenerateInput) (<-chan ai.StreamEvent, error) {
	f.mu.Lock()
	f.models = append(f.models, in.Model)
	i := f.idx
	f.idx++
	f.mu.Unlock()

	if f.blockCtx {
		ch := make(chan ai.StreamEvent)
		go func() {
			<-ctx.Done()
			ch <- ai.StreamEvent{Type: ai.StreamEventError, Error: ctx.Err()}
			close(ch)
		}()
		return ch, nil
	}
	if f.errAt == i+1 {
		return nil, fmt.Errorf("provider boom")
	}
	var t aiTurn
	if i < len(f.turns) {
		t = f.turns[i]
	}
	ch := make(chan ai.StreamEvent, 16)
	go func() {
		defer close(ch)
		if f.delay > 0 {
			time.Sleep(f.delay)
		}
		for _, r := range t.reasoning {
			ch <- ai.StreamEvent{Type: ai.StreamEventReasoning, Token: r}
		}
		for _, tok := range t.tokens {
			ch <- ai.StreamEvent{Type: ai.StreamEventToken, Token: tok}
		}
		if t.errEvent != nil {
			ch <- ai.StreamEvent{Type: ai.StreamEventError, Error: t.errEvent}
			return
		}
		done := ai.StreamEvent{Type: ai.StreamEventDone, FullText: t.fullText, AllToolCalls: t.tcs, FinishReason: t.finish}
		if !t.noUsage {
			u := t.usage
			if u == nil {
				u = &ai.Usage{}
			}
			done.Usage = u
		}
		ch <- done
	}()
	return ch, nil
}

func (f *fakeAI) GetAvaibleModels(ctx context.Context) ([]string, error) { return nil, nil }
func (f *fakeAI) GetModelsWithPricing(ctx context.Context) ([]ai.ModelInfo, error) {
	return nil, nil
}

type fakeDriver struct {
	model      string
	toolset    []tools.Definition
	dispatchFn func(call ai.ToolCall) StepResult
	finishFn   func(call ai.ToolCall) FinishResult
	progressFn func() Progress

	dispatched []string
	refreshes  int
	afterTurns int
}

func (d *fakeDriver) Model() string {
	if d.model == "" {
		return "m"
	}
	return d.model
}
func (d *fakeDriver) SystemPrompt() string      { return "sys" }
func (d *fakeDriver) Tools() []tools.Definition { return d.toolset }
func (d *fakeDriver) Reground(iter, maxIter, noMut int) string {
	return "obs"
}
func (d *fakeDriver) Refresh()            { d.refreshes++ }
func (d *fakeDriver) AfterTurn(emit Emit) { d.afterTurns++ }
func (d *fakeDriver) Progress() Progress {
	if d.progressFn != nil {
		return d.progressFn()
	}
	return Progress{}
}
func (d *fakeDriver) Dispatch(ctx context.Context, call ai.ToolCall, emit Emit) StepResult {
	d.dispatched = append(d.dispatched, call.Name)
	if d.dispatchFn != nil {
		return d.dispatchFn(call)
	}
	return StepResult{Result: "ok"}
}
func (d *fakeDriver) FinishVerdict(call ai.ToolCall) FinishResult {
	if d.finishFn != nil {
		return d.finishFn(call)
	}
	return FinishResult{Honored: true, Summary: "pronto", Result: "finish ACEITO"}
}

type capture struct {
	mu    sync.Mutex
	types []string
}

func (c *capture) emit(t string, _ interface{}) {
	c.mu.Lock()
	c.types = append(c.types, t)
	c.mu.Unlock()
}
func (c *capture) count(t string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, x := range c.types {
		if x == t {
			n++
		}
	}
	return n
}
func (c *capture) has(t string) bool { return c.count(t) > 0 }

func tcall(name string) ai.ToolCall { return ai.ToolCall{Name: name} }

// run is the standard harness for a single prompt.
func run(t *testing.T, prov *fakeAI, drv *fakeDriver, cfg Config) (Outcome, *capture, *Session) {
	t.Helper()
	e := Engine{AI: prov}
	cp := &capture{}
	sess := &Session{}
	out := e.Run(context.Background(), cp.emit, drv, cfg, sess, "faça X")
	return out, cp, sess
}

// ---- Run: terminal outcomes ----------------------------------------------

func TestRun_FinishHonored(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tcs: []ai.ToolCall{tcall("finish")}}}}
	drv := &fakeDriver{}
	out, cp, sess := run(t, prov, drv, Config{FinishToolName: "finish"})
	if out.Kind != OutcomeDone || !out.Valid || out.Summary != "pronto" {
		t.Fatalf("expected honored finish, got %+v", out)
	}
	if cp.count("tool") != 1 {
		t.Fatalf("expected one finish tool event, got %d", cp.count("tool"))
	}
	if drv.refreshes != 1 || drv.afterTurns != 1 {
		t.Fatalf("expected one refresh + one afterTurn, got %d/%d", drv.refreshes, drv.afterTurns)
	}
	if len(sess.History) != 3 || sess.History[0].Role != ai.RoleUser {
		t.Fatalf("unexpected history: %+v", sess.History)
	}
}

func TestRun_FinishMixedWithMutationIgnoredThenHonored(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{tcs: []ai.ToolCall{tcall("mut"), tcall("finish")}}, // finish ignored (a mutation happened)
		{tcs: []ai.ToolCall{tcall("finish")}},               // honored
	}}
	calls := 0
	drv := &fakeDriver{
		dispatchFn: func(ai.ToolCall) StepResult { calls++; return StepResult{Result: "did", Mutated: true} },
		progressFn: func() Progress { return Progress{Valid: true} },
	}
	out, _, _ := run(t, prov, drv, Config{FinishToolName: "finish"})
	if out.Kind != OutcomeDone || !out.Valid {
		t.Fatalf("expected eventual honored finish, got %+v", out)
	}
	if calls != 1 {
		t.Fatalf("expected exactly one mutation dispatch, got %d", calls)
	}
}

func TestRun_FinishRefusedThenRepairExhausted(t *testing.T) {
	turns := make([]aiTurn, 6)
	for i := range turns {
		turns[i] = aiTurn{tcs: []ai.ToolCall{tcall("finish")}}
	}
	prov := &fakeAI{turns: turns}
	drv := &fakeDriver{
		finishFn: func(ai.ToolCall) FinishResult {
			return FinishResult{Honored: false, Result: "RECUSADO", EventSummary: "recusado", PendingWork: 1}
		},
	}
	out, cp, _ := run(t, prov, drv, Config{FinishToolName: "finish", RepairBudget: 1, NoProgressStop: 20, MaxIterations: 20})
	if out.Kind != OutcomeDone || out.Valid || out.Summary != reasonRepairExhausted {
		t.Fatalf("expected repair-budget exhaustion, got %+v", out)
	}
	if cp.count("tool") < 3 {
		t.Fatalf("expected refused finish tool events, got %d", cp.count("tool"))
	}
}

func TestRun_NoProgressStallA(t *testing.T) {
	turns := make([]aiTurn, 6)
	for i := range turns {
		turns[i] = aiTurn{tcs: []ai.ToolCall{tcall("noop")}}
	}
	prov := &fakeAI{turns: turns}
	drv := &fakeDriver{
		dispatchFn: func(ai.ToolCall) StepResult { return StepResult{Result: "ok"} }, // never mutates
		progressFn: func() Progress { return Progress{StateHash: "H", Valid: false} },
	}
	out, _, _ := run(t, prov, drv, Config{FinishToolName: "finish", NoProgressStop: 3, MaxIterations: 20})
	if out.Kind != OutcomeDone || out.Summary != reasonNoProgressState {
		t.Fatalf("expected stall-A (state unchanged), got %+v", out)
	}
}

func TestRun_ChurnStallB(t *testing.T) {
	turns := make([]aiTurn, 6)
	for i := range turns {
		turns[i] = aiTurn{tcs: []ai.ToolCall{tcall("mut")}}
	}
	prov := &fakeAI{turns: turns}
	n := 0
	drv := &fakeDriver{
		dispatchFn: func(ai.ToolCall) StepResult { return StepResult{Result: "did", Mutated: true} },
		progressFn: func() Progress { n++; return Progress{StateHash: fmt.Sprintf("h%d", n), BlockingSignature: "SIG", Valid: false} },
	}
	out, _, _ := run(t, prov, drv, Config{FinishToolName: "finish", NoProgressStop: 2, MaxIterations: 20})
	if out.Kind != OutcomeDone || out.Summary != reasonChurn {
		t.Fatalf("expected stall-B (churn), got %+v", out)
	}
}

func TestRun_MaxIterations(t *testing.T) {
	turns := make([]aiTurn, 5)
	for i := range turns {
		turns[i] = aiTurn{tcs: []ai.ToolCall{tcall("mut")}}
	}
	prov := &fakeAI{turns: turns}
	n := 0
	drv := &fakeDriver{
		dispatchFn: func(ai.ToolCall) StepResult { return StepResult{Result: "did", Mutated: true} },
		progressFn: func() Progress { n++; return Progress{StateHash: fmt.Sprintf("h%d", n)} }, // changing hash, empty sig
	}
	out, _, _ := run(t, prov, drv, Config{FinishToolName: "finish", MaxIterations: 2, NoProgressStop: 20})
	if out.Kind != OutcomeDone || out.Summary != reasonMaxIterations {
		t.Fatalf("expected max-iterations, got %+v", out)
	}
}

func TestRun_TokenBudget(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tcs: []ai.ToolCall{tcall("mut")}, usage: &ai.Usage{TotalTokens: 100}}}}
	drv := &fakeDriver{dispatchFn: func(ai.ToolCall) StepResult { return StepResult{Mutated: true} }}
	out, _, sess := run(t, prov, drv, Config{FinishToolName: "finish", SessionTokenBudget: 50})
	if out.Kind != OutcomeDone || out.Summary != reasonTokenBudget {
		t.Fatalf("expected token-budget stop, got %+v", out)
	}
	if sess.TokensUsed != 100 {
		t.Fatalf("expected tokens tallied, got %d", sess.TokensUsed)
	}
}

func TestRun_ProviderError(t *testing.T) {
	prov := &fakeAI{errAt: 1}
	out, _, _ := run(t, prov, &fakeDriver{}, Config{FinishToolName: "finish"})
	if out.Kind != OutcomeDone || out.Valid || !strings.Contains(out.Summary, "provedor") {
		t.Fatalf("expected provider error, got %+v", out)
	}
}

func TestRun_StreamErrorEvent(t *testing.T) {
	// A mid-stream error event (not ctx-related) surfaces as a provider error.
	prov := &fakeAI{turns: []aiTurn{{reasoning: []string{"pensando."}, errEvent: errors.New("kaboom")}}}
	out, _, _ := run(t, prov, &fakeDriver{}, Config{FinishToolName: "finish"})
	if out.Kind != OutcomeDone || !strings.Contains(out.Summary, "provedor") {
		t.Fatalf("expected provider error from stream event, got %+v", out)
	}
}

func TestRun_ContextCancelledAtTop(t *testing.T) {
	e := Engine{AI: &fakeAI{turns: []aiTurn{{tcs: []ai.ToolCall{tcall("finish")}}}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := e.Run(ctx, (&capture{}).emit, &fakeDriver{}, Config{FinishToolName: "finish"}, &Session{}, "x")
	if out.Kind != OutcomeDone || out.Summary != reasonCancelled {
		t.Fatalf("expected cancelled, got %+v", out)
	}
}

func TestRun_ContextTimeoutDuringStream(t *testing.T) {
	e := Engine{AI: &fakeAI{blockCtx: true}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	out := e.Run(ctx, (&capture{}).emit, &fakeDriver{}, Config{FinishToolName: "finish"}, &Session{}, "x")
	if out.Kind != OutcomeDone || out.Summary != reasonTimeout {
		t.Fatalf("expected timeout, got %+v", out)
	}
}

func TestRun_CtxExpiresBetweenIterations(t *testing.T) {
	// The first turn sleeps past the deadline without checking ctx, so the SECOND
	// iteration's top-of-loop ctx check fires.
	prov := &fakeAI{turns: []aiTurn{{tcs: []ai.ToolCall{tcall("mut")}}}, delay: 40 * time.Millisecond}
	e := Engine{AI: prov}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	drv := &fakeDriver{dispatchFn: func(ai.ToolCall) StepResult { return StepResult{Mutated: true} }}
	out := e.Run(ctx, (&capture{}).emit, drv, Config{FinishToolName: "finish"}, &Session{}, "x")
	if out.Kind != OutcomeDone || out.Summary != reasonTimeout {
		t.Fatalf("expected timeout between iterations, got %+v", out)
	}
}

func TestRun_EmptyTurnTruncationGivesUp(t *testing.T) {
	turns := make([]aiTurn, 5) // all empty (no content, no tools)
	prov := &fakeAI{turns: turns}
	out, _, _ := run(t, prov, &fakeDriver{}, Config{FinishToolName: "finish", EmptyTurnRetries: 2, MaxIterations: 20})
	if out.Kind != OutcomeDone || out.Summary != reasonEmptyTurn {
		t.Fatalf("expected empty-turn give-up, got %+v", out)
	}
}

func TestRun_TruncatedByLengthThenRecovers(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{finish: "length"},                    // truncated → retry
		{tcs: []ai.ToolCall{tcall("finish")}}, // then finishes
	}}
	out, _, _ := run(t, prov, &fakeDriver{}, Config{FinishToolName: "finish", EmptyTurnRetries: 2})
	if out.Kind != OutcomeDone || !out.Valid {
		t.Fatalf("expected recovery after a truncated turn, got %+v", out)
	}
}

func TestRun_ConversationalIdle(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tokens: []string{"Olá!"}, finish: "stop"}}}
	drv := &fakeDriver{progressFn: func() Progress { return Progress{Valid: true} }}
	out, cp, sess := run(t, prov, drv, Config{FinishToolName: "finish"})
	if out.Kind != OutcomeIdle || !out.Valid {
		t.Fatalf("expected idle, got %+v", out)
	}
	if !cp.has("assistant_delta") {
		t.Fatal("expected the answer to be streamed")
	}
	if len(sess.History) != 2 || sess.History[1].Content != "Olá!" {
		t.Fatalf("expected the reply appended to history, got %+v", sess.History)
	}
}

func TestRun_Paused(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tcs: []ai.ToolCall{tcall("create_agent")}}}}
	drv := &fakeDriver{
		dispatchFn: func(ai.ToolCall) StepResult {
			return StepResult{Result: "proposta registrada", Pause: &Pause{Reason: "aguardando aprovação", Payload: "act-1"}}
		},
	}
	out, _, sess := run(t, prov, drv, Config{FinishToolName: "finish"})
	if out.Kind != OutcomePaused || out.Summary != "aguardando aprovação" {
		t.Fatalf("expected paused outcome, got %+v", out)
	}
	if out.Pause == nil || out.Pause.Payload != "act-1" {
		t.Fatalf("expected pause payload, got %+v", out.Pause)
	}
	if drv.afterTurns != 0 {
		t.Fatal("AfterTurn must not run when the loop pauses pre-commit")
	}
	if len(sess.History) != 3 || sess.History[2].Content != "proposta registrada" {
		t.Fatalf("unexpected paused history: %+v", sess.History)
	}
}

func TestRun_MultiToolCallsAndIDNormalization(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{
		{tcs: []ai.ToolCall{tcall("a"), tcall("b")}},
		{tcs: []ai.ToolCall{tcall("finish")}},
	}}
	drv := &fakeDriver{
		dispatchFn: func(c ai.ToolCall) StepResult { return StepResult{Result: "r-" + c.Name, Mutated: true} },
		progressFn: func() Progress { return Progress{Valid: true, StateHash: "x"} },
	}
	out, _, sess := run(t, prov, drv, Config{FinishToolName: "finish"})
	if out.Kind != OutcomeDone {
		t.Fatalf("expected done, got %+v", out)
	}
	if len(drv.dispatched) != 2 || drv.dispatched[0] != "a" || drv.dispatched[1] != "b" {
		t.Fatalf("expected both tools dispatched, got %+v", drv.dispatched)
	}
	var toolMsgs int
	for _, m := range sess.History {
		if m.Role == ai.RoleTool {
			if m.ToolCallID == "" {
				t.Fatal("tool result missing a tool-call id")
			}
			toolMsgs++
		}
	}
	if toolMsgs == 0 {
		t.Fatal("expected tool result messages in history")
	}
}

func TestRun_DefaultModelPropagated(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tcs: []ai.ToolCall{tcall("finish")}}}}
	run(t, prov, &fakeDriver{model: "prov/x"}, Config{FinishToolName: "finish"})
	prov.mu.Lock()
	defer prov.mu.Unlock()
	if len(prov.models) == 0 || prov.models[0] != "prov/x" {
		t.Fatalf("expected driver model used, got %+v", prov.models)
	}
}

func TestRun_LogPrefixCorrelation(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tcs: []ai.ToolCall{tcall("finish")}}}}
	e := Engine{AI: prov}
	out := e.Run(context.Background(), (&capture{}).emit, &fakeDriver{},
		Config{FinishToolName: "finish", LogPrefix: "[test] s1"}, &Session{}, "x")
	if out.Kind != OutcomeDone {
		t.Fatalf("expected done with a log prefix set, got %+v", out)
	}
}

// ---- streamGenerate ------------------------------------------------------

func TestStreamGenerate_TokensReasoningUsage(t *testing.T) {
	e := Engine{AI: &fakeAI{turns: []aiTurn{{
		reasoning: []string{"curto", "agora com ponto."}, // first buffered, second flushed
		tokens:    []string{"abc", "def.", "gh"},         // batched + punctuation flush
		tcs:       []ai.ToolCall{tcall("t")},
		finish:    "tool_calls",
		usage:     &ai.Usage{TotalTokens: 9},
	}}}}
	cp := &capture{}
	out, err := e.streamGenerate(context.Background(), cp.emit, ai.GenerateInput{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Message.Content != "abcdef.gh" {
		t.Fatalf("content assembly wrong: %q", out.Message.Content)
	}
	if len(out.ToolCalls) != 1 || out.FinishReason != "tool_calls" || out.Usage.TotalTokens != 9 {
		t.Fatalf("done fields wrong: %+v", out)
	}
	if !cp.has("reasoning_delta") || !cp.has("reasoning_done") || !cp.has("assistant_delta") {
		t.Fatalf("expected reasoning + answer deltas, got %+v", cp.types)
	}
}

func TestStreamGenerate_FullTextFallbackNoUsage(t *testing.T) {
	e := Engine{AI: &fakeAI{turns: []aiTurn{{fullText: "fallback", noUsage: true}}}}
	out, err := e.streamGenerate(context.Background(), (&capture{}).emit, ai.GenerateInput{})
	if err != nil || out.Message.Content != "fallback" || out.Usage.TotalTokens != 0 {
		t.Fatalf("expected fulltext fallback + zero usage, got %+v err=%v", out, err)
	}
}

func TestStreamGenerate_OpenError(t *testing.T) {
	e := Engine{AI: &fakeAI{errAt: 1}}
	if _, err := e.streamGenerate(context.Background(), (&capture{}).emit, ai.GenerateInput{}); err == nil {
		t.Fatal("expected open error")
	}
}

func TestStreamGenerate_ErrorEventAfterReasoning(t *testing.T) {
	e := Engine{AI: &fakeAI{turns: []aiTurn{{reasoning: []string{"x"}, tokens: []string{"y"}, errEvent: errors.New("boom")}}}}
	if _, err := e.streamGenerate(context.Background(), (&capture{}).emit, ai.GenerateInput{}); err == nil {
		t.Fatal("expected stream error")
	}
}

// ---- pure helpers --------------------------------------------------------

func TestWithDefaults(t *testing.T) {
	d := Config{}.withDefaults()
	if d.MaxIterations != 30 || d.NoProgressStop != 5 || d.RepairBudget != 3 ||
		d.EmptyTurnRetries != 2 || d.MaxHistoryMsgs != 80 || d.MaxTokensPerGen != 24000 ||
		d.ReasoningMaxTokens != 10000 || d.FinishToolName != "finish" ||
		d.RepeatedTurnStop != 3 {
		t.Fatalf("defaults wrong: %+v", d)
	}
	in := Config{MaxIterations: 1, NoProgressStop: 1, RepairBudget: 1, EmptyTurnRetries: 1,
		MaxHistoryMsgs: 1, MaxTokensPerGen: 1, ReasoningMaxTokens: 1, FinishToolName: "fim",
		RepeatedTurnStop: 1}
	if out := in.withDefaults(); out != in {
		t.Fatalf("populated config must be unchanged, got %+v", out)
	}
}

func TestSessionEndReason(t *testing.T) {
	if sessionEndReason(context.DeadlineExceeded) != reasonTimeout {
		t.Fatal("deadline → timeout")
	}
	if sessionEndReason(context.Canceled) != reasonCancelled {
		t.Fatal("cancel → cancelled")
	}
}

func TestTrimHistory(t *testing.T) {
	u := ai.Message{Role: ai.RoleUser, Content: "u"}
	a := ai.Message{Role: ai.RoleAssistant, Content: "a"}
	tl := ai.Message{Role: ai.RoleTool, Content: "t"}

	if got := trimHistory([]ai.Message{u, a}, 5); len(got) != 2 {
		t.Fatalf("under cap should be unchanged, got %d", len(got))
	}
	if got := trimHistory([]ai.Message{u, a, a, a, a}, 4); len(got) != 4 {
		t.Fatalf("start<=1 branch: got %d", len(got))
	}
	h := []ai.Message{u, a, tl, a, tl, a, tl}
	got := trimHistory(h, 3)
	if got[0].Role != ai.RoleUser {
		t.Fatalf("first message (user request) must be retained, got %+v", got)
	}
	if got[1].Role == ai.RoleTool {
		t.Fatalf("suffix must not start with an orphan tool result, got %+v", got)
	}
	h2 := []ai.Message{a, a, a, a, a, a}
	if got := trimHistory(h2, 3); got[0].Role == ai.RoleUser {
		t.Fatal("must not invent a user anchor when none exists")
	}
}

func TestRecordTurn(t *testing.T) {
	sess := &Session{}
	calls := []ai.ToolCall{{ID: "c1", Name: "a"}, {ID: "c2", Name: "b"}}
	recordTurn(sess, 80, "said", calls, []string{""}) // fewer results than calls + empty result
	if len(sess.History) != 3 {
		t.Fatalf("expected assistant + 2 tool results, got %d", len(sess.History))
	}
	if sess.History[1].Content != "ok" || sess.History[2].Content != "ok" {
		t.Fatalf("empty/missing results must normalize to ok, got %+v", sess.History[1:])
	}
	if sess.History[1].ToolCallID != "c1" || sess.History[2].ToolCallID != "c2" {
		t.Fatalf("tool-call ids must be preserved, got %+v", sess.History[1:])
	}
}
