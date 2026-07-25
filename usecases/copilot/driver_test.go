package copilot_usecase

import (
	"context"
	"strings"
	"sync"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	"vozko/usecases/agentloop"
)

// The driver is tested against a generic fakeTool — it cares only about a tool's
// Meta (read vs mutating, RBAC resource/action), not about any concrete tool. The
// real agent tools are tested in package copilottools.

type scriptAI struct {
	turns [][]ai.ToolCall
	texts []string
	idx   int
}

func (s *scriptAI) Generate(ctx context.Context, in ai.GenerateInput) (*ai.GenerateOutput, error) {
	return nil, context.Canceled
}
func (s *scriptAI) GenerateStream(ctx context.Context, in ai.GenerateInput) (<-chan ai.StreamEvent, error) {
	i := s.idx
	s.idx++
	var tcs []ai.ToolCall
	var txt string
	if i < len(s.turns) {
		tcs = s.turns[i]
	}
	if i < len(s.texts) {
		txt = s.texts[i]
	}
	ch := make(chan ai.StreamEvent, 4)
	go func() {
		defer close(ch)
		if txt != "" {
			ch <- ai.StreamEvent{Type: ai.StreamEventToken, Token: txt}
		}
		ch <- ai.StreamEvent{Type: ai.StreamEventDone, FullText: txt, AllToolCalls: tcs, Usage: &ai.Usage{}}
	}()
	return ch, nil
}
func (s *scriptAI) GetAvaibleModels(ctx context.Context) ([]string, error)          { return nil, nil }
func (s *scriptAI) GetModelsWithPricing(ctx context.Context) ([]ai.ModelInfo, error) { return nil, nil }

type fakeAccess struct {
	err     error
	lastRes workspace.Resource
	lastAct workspace.Action
}

func (f *fakeAccess) Execute(userID, wsID string, res workspace.Resource, act workspace.Action) error {
	f.lastRes = res
	f.lastAct = act
	return f.err
}

type fakeTool struct {
	name    string
	meta    copilot.Meta
	result  copilot.Result
	calls   int
	gotCC   copilot.Context
	gotArgs map[string]interface{}
}

func (f *fakeTool) Definition() tools.Definition { return tools.Definition{Name: f.name, Description: "x"} }
func (f *fakeTool) Meta() copilot.Meta           { return f.meta }
func (f *fakeTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	f.calls++
	f.gotCC = cc
	f.gotArgs = args
	if f.result.Status == "" {
		return copilot.Result{Status: copilot.StatusOK, Data: "done"}
	}
	return f.result
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
func (c *capture) has(t string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, x := range c.types {
		if x == t {
			return true
		}
	}
	return false
}

func call(name string, args map[string]interface{}) ai.ToolCall {
	return ai.ToolCall{Name: name, Arguments: args}
}

var (
	ownerCtx  = copilot.Context{WorkspaceID: "ws1", UserID: "u1", Role: workspace.RoleOwner}
	readMeta  = copilot.Meta{Mutating: false, Resource: workspace.ResourceAgents, Action: workspace.ActionRead}
	writeMeta = copilot.Meta{Mutating: true, Resource: workspace.ResourceAgents, Action: workspace.ActionCreate}
)

func driverWith(fa *fakeAccess, ts ...copilot.Tool) *Driver {
	return NewDriver(ownerCtx, "m", NewRegistry(ts...), fa, func() string { return "act-1" })
}

// ---- reuse on the real engine --------------------------------------------

func TestDriver_ReadExecutesAndScopes(t *testing.T) {
	rt := &fakeTool{name: "read_x", meta: readMeta}
	fa := &fakeAccess{}
	drv := driverWith(fa, rt)
	e := agentloop.Engine{AI: &scriptAI{
		turns: [][]ai.ToolCall{{call("read_x", map[string]interface{}{"q": "hi"})}, {}},
		texts: []string{"", "pronto"},
	}}
	out := e.Run(context.Background(), (&capture{}).emit, drv, DefaultConfig(ownerCtx, 0), &agentloop.Session{}, "leia")
	if out.Kind != agentloop.OutcomeIdle {
		t.Fatalf("expected idle after a read + reply, got %+v", out)
	}
	if rt.calls != 1 || rt.gotCC.WorkspaceID != "ws1" {
		t.Fatalf("read must execute, scoped to the session, got calls=%d cc=%+v", rt.calls, rt.gotCC)
	}
	if fa.lastRes != workspace.ResourceAgents || fa.lastAct != workspace.ActionRead {
		t.Fatalf("RBAC must be checked for the tool's resource:action, got %s:%s", fa.lastRes, fa.lastAct)
	}
}

func TestDriver_MutationPausesForApproval(t *testing.T) {
	wt := &fakeTool{name: "write_x", meta: writeMeta}
	drv := driverWith(&fakeAccess{}, wt)
	e := agentloop.Engine{AI: &scriptAI{turns: [][]ai.ToolCall{{call("write_x", map[string]interface{}{"a": 1})}}}}
	cp := &capture{}
	out := e.Run(context.Background(), cp.emit, drv, DefaultConfig(ownerCtx, 0), &agentloop.Session{}, "crie")
	if out.Kind != agentloop.OutcomePaused {
		t.Fatalf("expected paused for approval, got %+v", out)
	}
	pa, ok := out.Pause.Payload.(copilot.PendingAction)
	if !ok || pa.ToolName != "write_x" || pa.ID != "act-1" {
		t.Fatalf("expected a pending action, got %+v", out.Pause)
	}
	if wt.calls != 0 {
		t.Fatal("a mutation must NOT execute before approval")
	}
	if !cp.has("tool_proposal") {
		t.Fatal("a proposal event must be emitted")
	}
}

// ---- driver units --------------------------------------------------------

func TestDriver_RBACDeniedDoesNotExecuteOrPause(t *testing.T) {
	rt := &fakeTool{name: "read_x", meta: readMeta}
	drv := driverWith(&fakeAccess{err: workspace.ErrInsufficientPermissions}, rt)
	cp := &capture{}
	step := drv.Dispatch(context.Background(), call("read_x", nil), cp.emit)
	if rt.calls != 0 || step.Pause != nil || !strings.Contains(step.Result, "PERMISSÃO") {
		t.Fatalf("a denied call must not execute or pause, got calls=%d step=%+v", rt.calls, step)
	}
	if !cp.has("tool") {
		t.Fatal("a denied tool event should be emitted")
	}
}

func TestDriver_ExecuteApprovedRunsToolScoped(t *testing.T) {
	wt := &fakeTool{name: "write_x", meta: writeMeta}
	drv := driverWith(&fakeAccess{}, wt)
	res := drv.ExecuteApproved(context.Background(), copilot.PendingAction{ToolName: "write_x", Args: map[string]interface{}{"a": 1}})
	if res.Status != copilot.StatusOK || wt.calls != 1 || wt.gotCC.WorkspaceID != "ws1" {
		t.Fatalf("approval must run the tool scoped to the session, got %+v calls=%d", res, wt.calls)
	}
}

func TestDriver_ExecuteApprovedSurfacesError(t *testing.T) {
	wt := &fakeTool{name: "write_x", meta: writeMeta, result: copilot.Result{Status: copilot.StatusError, Message: "inválido"}}
	drv := driverWith(&fakeAccess{}, wt)
	if drv.ExecuteApproved(context.Background(), copilot.PendingAction{ToolName: "write_x"}).Status != copilot.StatusError {
		t.Fatal("tool errors must surface from approval")
	}
}

func TestDriver_ExecuteApprovedReChecksRBAC(t *testing.T) {
	wt := &fakeTool{name: "write_x", meta: writeMeta}
	drv := driverWith(&fakeAccess{err: workspace.ErrInsufficientPermissions}, wt)
	res := drv.ExecuteApproved(context.Background(), copilot.PendingAction{ToolName: "write_x"})
	if res.Status != copilot.StatusDenied || wt.calls != 0 {
		t.Fatalf("approval must re-check RBAC and not execute when denied, got %+v calls=%d", res, wt.calls)
	}
}

func TestDriver_UnknownTool(t *testing.T) {
	drv := driverWith(&fakeAccess{})
	step := drv.Dispatch(context.Background(), call("nope", nil), (&capture{}).emit)
	if step.Pause != nil || !strings.Contains(step.Result, "desconhecida") {
		t.Fatalf("unknown tool should be reported, not paused, got %+v", step)
	}
	if drv.ExecuteApproved(context.Background(), copilot.PendingAction{ToolName: "nope"}).Status != copilot.StatusError {
		t.Fatal("approving an unknown tool must error")
	}
}

func TestDriver_Accessors(t *testing.T) {
	drv := driverWith(&fakeAccess{}, &fakeTool{name: "a", meta: readMeta}, &fakeTool{name: "b", meta: writeMeta})
	if drv.Model() != "m" {
		t.Fatal("model")
	}
	if !strings.Contains(drv.SystemPrompt(), "aprovação") {
		t.Fatal("system prompt should mention approval")
	}
	if len(drv.Tools()) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(drv.Tools()))
	}
	if !strings.Contains(drv.Reground("faça X", 1, 12, 0), "faça X") {
		t.Fatal("reground should restate the request")
	}
	if !drv.Progress().Valid {
		t.Fatal("copilot progress is always valid (no validator)")
	}
	drv.Refresh()
	drv.AfterTurn(func(string, interface{}) {})
	if fv := drv.FinishVerdict(call("finish", map[string]interface{}{"summary": "feito"})); !fv.Honored || fv.Summary != "feito" {
		t.Fatalf("finish should be honored with its summary, got %+v", fv)
	}
	if drv.FinishVerdict(call("finish", nil)).Summary != "concluído" {
		t.Fatal("finish should default its summary")
	}
}

func TestDriver_MintID(t *testing.T) {
	if NewDriver(ownerCtx, "m", NewRegistry(), &fakeAccess{}, nil).mintID() != "act" {
		t.Fatal("nil id generator should fall back")
	}
	if NewDriver(ownerCtx, "m", NewRegistry(), &fakeAccess{}, func() string { return "z" }).mintID() != "z" {
		t.Fatal("custom id generator should be used")
	}
}

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig(ownerCtx, 1234)
	if c.WorkspaceID != "ws1" || c.SessionTokenBudget != 1234 || c.FinishToolName != "finish" || c.MaxIterations != 12 {
		t.Fatalf("unexpected config: %+v", c)
	}
}

func TestRegistry(t *testing.T) {
	reg := NewRegistry(&fakeTool{name: "a", meta: readMeta}, nil)
	if _, ok := reg.Get("a"); !ok {
		t.Fatal("a should be registered")
	}
	if _, ok := reg.Get("nope"); ok {
		t.Fatal("unknown tool")
	}
	if len(reg.Definitions()) != 1 {
		t.Fatalf("nil tools must be skipped, got %d defs", len(reg.Definitions()))
	}
}

func TestRenderResult(t *testing.T) {
	if got := renderResult(copilot.Result{Status: copilot.StatusOK, Data: map[string]int{"n": 1}}); got != `{"n":1}` {
		t.Fatalf("ok with data → json, got %q", got)
	}
	if renderResult(copilot.Result{Status: copilot.StatusOK}) != "ok" {
		t.Fatal("ok with nil data → ok")
	}
	if renderResult(copilot.Result{Status: copilot.StatusOK, Data: make(chan int)}) != "ok" {
		t.Fatal("unmarshalable data → ok")
	}
	if got := renderResult(copilot.Result{Status: copilot.StatusError, Message: "boom"}); got != "error: boom" {
		t.Fatalf("error with message, got %q", got)
	}
	if got := renderResult(copilot.Result{Status: copilot.StatusDenied}); got != "denied" {
		t.Fatalf("status only, got %q", got)
	}
}

func TestSummarizeAndToolEvent(t *testing.T) {
	if !strings.Contains(summarizeCall(call("create_agent", map[string]interface{}{"name": "B"})), "create_agent") {
		t.Fatal("summary includes tool name")
	}
	if summarizeCall(call("t", map[string]interface{}{"bad": make(chan int)})) != "t" {
		t.Fatal("unmarshalable args → name only")
	}
	ev := toolEvent("t", "s", true)
	if ev["name"] != "t" || ev["ok"] != true {
		t.Fatalf("tool event fields, got %+v", ev)
	}
}
