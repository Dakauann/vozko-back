package copilot_usecase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	"vozko/usecases/agentloop"
)

type scriptAI struct {
	turns  [][]ai.ToolCall
	texts  []string
	usage  *ai.Usage
	idx    int
	inputs []ai.GenerateInput
	models []ai.ModelInfo
}

func (s *scriptAI) Generate(ctx context.Context, in ai.GenerateInput) (*ai.GenerateOutput, error) {
	return nil, context.Canceled
}
func (s *scriptAI) GenerateStream(ctx context.Context, in ai.GenerateInput) (<-chan ai.StreamEvent, error) {
	s.inputs = append(s.inputs, in)
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
		ch <- ai.StreamEvent{Type: ai.StreamEventDone, FullText: txt, AllToolCalls: tcs, Usage: s.usageOrEmpty()}
	}()
	return ch, nil
}
func (s *scriptAI) GetAvaibleModels(ctx context.Context) ([]string, error) { return nil, nil }
func (s *scriptAI) GetModelsWithPricing(ctx context.Context) ([]ai.ModelInfo, error) {
	return s.models, nil
}

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
	invalid error
}

func (f *fakeTool) Validate(context.Context, copilot.Context, map[string]interface{}) error {
	return f.invalid
}

func (f *fakeTool) Definition() tools.Definition {
	return tools.Definition{Name: f.name, Description: "x"}
}
func (f *fakeTool) Meta() copilot.Meta { return f.meta }
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

func driverWith(fa AccessChecker, ts ...copilot.Tool) *Driver {
	return NewDriver(ownerCtx, "m", NewRegistry(ts...), fa, openFunds{}, func() string { return "act-1" })
}

func TestDriver_ReadExecutesAndScopes(t *testing.T) {
	rt := &fakeTool{name: "read_x", meta: readMeta}
	fa := &fakeAccess{}
	drv := driverWith(fa, rt)
	e := agentloop.Engine{AI: &scriptAI{
		turns: [][]ai.ToolCall{{call("read_x", map[string]interface{}{"q": "hi"})}, {}},
		texts: []string{"", "pronto"},
	}}
	out := e.Run(context.Background(), (&capture{}).emit, drv, DefaultConfig(ownerCtx, ai.ModelInfo{}, 0), &agentloop.Session{}, "leia")
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
	out := e.Run(context.Background(), cp.emit, drv, DefaultConfig(ownerCtx, ai.ModelInfo{}, 0), &agentloop.Session{}, "crie")
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
	res := drv.ExecuteApproved(context.Background(), copilot.PendingAction{ToolName: "write_x", Args: map[string]interface{}{"a": 1}}, copilot.Approval{}, (&capture{}).emit)
	if res.Status != copilot.StatusOK || wt.calls != 1 || wt.gotCC.WorkspaceID != "ws1" {
		t.Fatalf("approval must run the tool scoped to the session, got %+v calls=%d", res, wt.calls)
	}
}

func TestDriver_ExecuteApprovedSurfacesError(t *testing.T) {
	wt := &fakeTool{name: "write_x", meta: writeMeta, result: copilot.Result{Status: copilot.StatusError, Message: "inválido"}}
	drv := driverWith(&fakeAccess{}, wt)
	if drv.ExecuteApproved(context.Background(), copilot.PendingAction{ToolName: "write_x"}, copilot.Approval{}, (&capture{}).emit).Status != copilot.StatusError {
		t.Fatal("tool errors must surface from approval")
	}
}

func TestDriver_ExecuteApprovedReChecksRBAC(t *testing.T) {
	wt := &fakeTool{name: "write_x", meta: writeMeta}
	drv := driverWith(&fakeAccess{err: workspace.ErrInsufficientPermissions}, wt)
	res := drv.ExecuteApproved(context.Background(), copilot.PendingAction{ToolName: "write_x"}, copilot.Approval{}, (&capture{}).emit)
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
	if drv.ExecuteApproved(context.Background(), copilot.PendingAction{ToolName: "nope"}, copilot.Approval{}, (&capture{}).emit).Status != copilot.StatusError {
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
	if strings.Contains(drv.Reground(1, 12, 0), "faça X") {
		t.Fatal("reground must not restate the user's request")
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
	if NewDriver(ownerCtx, "m", NewRegistry(), &fakeAccess{}, openFunds{}, nil).mintID() != "act" {
		t.Fatal("nil id generator should fall back")
	}
	if NewDriver(ownerCtx, "m", NewRegistry(), &fakeAccess{}, openFunds{}, func() string { return "z" }).mintID() != "z" {
		t.Fatal("custom id generator should be used")
	}
}

func TestAKnownModelIsLimitedByItsOwnWindowAndByMoney(t *testing.T) {
	model := ai.ModelInfo{ID: "m", ContextLength: 400_000, PromptPrice: 1.25, CompletionPrice: 10}
	c := DefaultConfig(ownerCtx, model, 2_000_000)
	if c.WorkspaceID != "ws1" || c.FinishToolName != "finish" || c.MaxIterations != answerMaxIterations {
		t.Fatalf("unexpected config: %+v", c)
	}
	if c.ModelLimits != model || c.CostCeilingMicros != 2_000_000 || c.SessionTokenBudget != 0 || c.GraceInstruction == "" {
		t.Fatalf("a known model must be bounded by its window and the cost ceiling: %+v", c)
	}
}

func TestAnUnknownModelKeepsTheTokenBudget(t *testing.T) {
	c := DefaultConfig(ownerCtx, ai.ModelInfo{ID: "m"}, 2_000_000)
	if c.SessionTokenBudget != AnswerTokenBudget || c.CostCeilingMicros != 0 || c.GraceInstruction == "" {
		t.Fatalf("without a cataloged window and price the token budget must stay: %+v", c)
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
	ev := toolStep{Name: "t", Summary: "s", Ok: true}.payload()
	if ev["name"] != "t" || ev["ok"] != true {
		t.Fatalf("tool event fields, got %+v", ev)
	}
}

func TestDriver_InvalidProposalNeverReachesTheUser(t *testing.T) {
	wt := &fakeTool{name: "write_x", meta: writeMeta, invalid: errors.New("business_phone_id \"x\" não existe")}
	cp := &capture{}
	step := driverWith(&fakeAccess{}, wt).Dispatch(context.Background(), call("write_x", nil), cp.emit)
	if step.Pause != nil || cp.has("tool_proposal") || wt.calls != 0 {
		t.Fatalf("an invalid proposal was shown or run: step=%+v", step)
	}
	if !strings.Contains(step.Result, "business_phone_id") || !strings.Contains(step.Result, "nunca invente") {
		t.Fatalf("the model must learn why, got %q", step.Result)
	}
}

func TestDriver_ChangeWithoutPreflightIsRefused(t *testing.T) {
	type bare struct{ copilot.Tool }
	wt := bare{&fakeTool{name: "write_x", meta: writeMeta}}
	cp := &capture{}
	step := driverWith(&fakeAccess{}, wt).Dispatch(context.Background(), call("write_x", nil), cp.emit)
	if step.Pause != nil || cp.has("tool_proposal") {
		t.Fatalf("a change without a validator was proposed: %+v", step)
	}
}

type resourceAccess map[workspace.Resource]bool

func (r resourceAccess) Execute(_, _ string, res workspace.Resource, _ workspace.Action) error {
	if r[res] {
		return nil
	}
	return workspace.ErrInsufficientPermissions
}

func TestDriver_OffersOnlyTheToolsThePersonMayUse(t *testing.T) {
	allowed := &fakeTool{name: "list_agents", meta: copilot.Meta{Resource: workspace.ResourceAgents, Action: workspace.ActionRead}}
	forbidden := &fakeTool{name: "create_template", meta: copilot.Meta{Mutating: true, Resource: workspace.ResourceWhatsAppTemplates, Action: workspace.ActionCreate}}
	drv := driverWith(resourceAccess{workspace.ResourceAgents: true}, allowed, forbidden)
	defs := drv.Tools()
	if len(defs) != 1 || defs[0].Name != "list_agents" {
		t.Fatalf("tools = %+v", defs)
	}
	admin := NewDriver(copilot.Context{WorkspaceID: "ws1", UserID: "u1", SystemAdmin: true}, "m", NewRegistry(allowed, forbidden), resourceAccess{}, openFunds{}, func() string { return "a" })
	if len(admin.Tools()) != 2 {
		t.Fatalf("system admin tools = %d", len(admin.Tools()))
	}
}

func TestDriver_AnAccessErrorHidesTheTool(t *testing.T) {
	drv := driverWith(&fakeAccess{err: errors.New("db down")}, &fakeTool{name: "a", meta: readMeta})
	if len(drv.Tools()) != 0 {
		t.Fatal("a tool whose permission could not be checked was offered")
	}
}

type secretTool struct {
	fakeTool
}

func (s *secretTool) Secrets(map[string]interface{}) []copilot.SecretField {
	return []copilot.SecretField{{Key: "password", Label: "Senha da linha"}}
}

func TestDriver_AProposalAsksForSecretsWithoutCarryingThem(t *testing.T) {
	st := &secretTool{fakeTool{name: "create_line", meta: writeMeta}}
	drv := driverWith(&fakeAccess{}, st)
	step := drv.Dispatch(context.Background(), call("create_line", map[string]interface{}{"name": "Principal", "password": "typed-in-chat"}), (&capture{}).emit)
	pa, ok := step.Pause.Payload.(copilot.PendingAction)
	if !ok {
		t.Fatalf("expected a proposal, got %+v", step)
	}
	if _, leaked := pa.Args["password"]; leaked {
		t.Fatal("the proposal stored a secret the model sent")
	}
	if len(pa.Secrets) != 1 || pa.Secrets[0].Key != "password" {
		t.Fatalf("secrets = %+v, want the password field", pa.Secrets)
	}
	for _, field := range pa.Fields {
		if field.Key == "password" {
			t.Fatal("the approval card shows the secret")
		}
	}
}

func TestDriver_ApprovalSuppliesTheSecretOnlyToTheTool(t *testing.T) {
	st := &secretTool{fakeTool{name: "create_line", meta: writeMeta}}
	drv := driverWith(&fakeAccess{}, st)
	pa := copilot.PendingAction{ToolName: "create_line", Args: map[string]interface{}{"name": "Principal"}, Secrets: st.Secrets(nil)}

	res := drv.ExecuteApproved(context.Background(), pa, copilot.Approval{Secrets: map[string]string{"password": "s3nh4"}}, (&capture{}).emit)

	if res.Status != copilot.StatusOK || st.gotArgs["password"] != "s3nh4" {
		t.Fatalf("res=%+v args=%v", res, st.gotArgs)
	}
	if _, leaked := pa.Args["password"]; leaked {
		t.Fatal("the stored proposal gained the secret")
	}
}

func TestDriver_ApprovalWithoutTheSecretChangesNothing(t *testing.T) {
	st := &secretTool{fakeTool{name: "create_line", meta: writeMeta}}
	drv := driverWith(&fakeAccess{}, st)
	res := drv.ExecuteApproved(context.Background(), copilot.PendingAction{ToolName: "create_line", Secrets: st.Secrets(nil)}, copilot.Approval{}, (&capture{}).emit)
	if res.Status != copilot.StatusError || st.calls != 0 {
		t.Fatalf("res=%+v calls=%d: a missing secret must stop the change", res, st.calls)
	}
}

func TestDriver_ApprovalChecksTheChangeAgainBeforeRunningIt(t *testing.T) {
	wt := &fakeTool{name: "assign", meta: writeMeta}
	drv := driverWith(&fakeAccess{}, wt)
	wt.invalid = errors.New("a conversa saiu do seu alcance")

	res := drv.ExecuteApproved(context.Background(), copilot.PendingAction{ToolName: "assign", Args: map[string]interface{}{"entry_id": "e1"}}, copilot.Approval{}, (&capture{}).emit)

	if res.Status != copilot.StatusError || wt.calls != 0 || !strings.Contains(res.Message, "saiu do seu alcance") {
		t.Fatalf("res=%+v calls=%d: a change that no longer passes its check must not run", res, wt.calls)
	}
}

type headerSecretTool struct {
	fakeTool
}

func (h *headerSecretTool) Secrets(args map[string]interface{}) []copilot.SecretField {
	headers, _ := args["headers"].(map[string]interface{})
	var out []copilot.SecretField
	for name := range headers {
		out = append(out, copilot.SecretField{Key: "header:" + name, Label: name})
	}
	return out
}

func (h *headerSecretTool) Conceal(args map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range args {
		out[k] = v
	}
	if headers, ok := args["headers"].(map[string]interface{}); ok {
		blank := map[string]interface{}{}
		for name := range headers {
			blank[name] = ""
		}
		out["headers"] = blank
	}
	return out
}

func TestDriver_SecretsNestedInAProposalAreAskedForAndNeverStored(t *testing.T) {
	ht := &headerSecretTool{fakeTool{name: "configure", meta: writeMeta}}
	drv := driverWith(&fakeAccess{}, ht)
	step := drv.Dispatch(context.Background(), call("configure", map[string]interface{}{
		"headers": map[string]interface{}{"Authorization": "Bearer from-the-model"},
	}), (&capture{}).emit)
	pa, ok := step.Pause.Payload.(copilot.PendingAction)
	if !ok {
		t.Fatalf("expected a proposal, got %+v", step)
	}
	if headers := pa.Args["headers"].(map[string]interface{}); headers["Authorization"] != "" {
		t.Fatalf("the stored proposal kept a nested secret: %v", headers)
	}
	if len(pa.Secrets) != 1 || pa.Secrets[0].Key != "header:Authorization" {
		t.Fatalf("secrets = %+v", pa.Secrets)
	}

	res := drv.ExecuteApproved(context.Background(), pa, copilot.Approval{Secrets: map[string]string{"header:Authorization": "Bearer typed"}}, (&capture{}).emit)
	if res.Status != copilot.StatusOK || ht.gotArgs["header:Authorization"] != "Bearer typed" {
		t.Fatalf("res=%+v args=%v", res, ht.gotArgs)
	}
}

type choiceTool struct {
	fakeTool
	asked   []copilot.Context
	choices []copilot.ChoiceField
	err     error
}

func (c *choiceTool) Choices(_ context.Context, cc copilot.Context, _ map[string]interface{}) ([]copilot.ChoiceField, error) {
	c.asked = append(c.asked, cc)
	return c.choices, c.err
}

func imageModelChoice() []copilot.ChoiceField {
	return []copilot.ChoiceField{{Key: "image_model", Kind: copilot.ChoiceImageModel}}
}

func TestDriver_TheToolSeesTheChatModel(t *testing.T) {
	ct := &choiceTool{fakeTool: fakeTool{name: "generate_image", meta: writeMeta}}
	drv := driverWith(&fakeAccess{}, ct)
	drv.Dispatch(context.Background(), ai.ToolCall{Name: "generate_image", Arguments: map[string]interface{}{}}, (&capture{}).emit)
	if len(ct.asked) != 1 || ct.asked[0].Model != "m" {
		t.Fatalf("asked %+v", ct.asked)
	}
}

func TestDriver_AProposalAsksForChoicesAndDropsTheModelGuess(t *testing.T) {
	ct := &choiceTool{fakeTool: fakeTool{name: "generate_image", meta: writeMeta}, choices: imageModelChoice()}
	drv := driverWith(&fakeAccess{}, ct)
	step := drv.Dispatch(context.Background(), ai.ToolCall{Name: "generate_image", Arguments: map[string]interface{}{"prompt": "pizza", "image_model": "x/invented"}}, (&capture{}).emit)
	pa, ok := step.Pause.Payload.(copilot.PendingAction)
	if !ok {
		t.Fatalf("expected a proposal, got %+v", step)
	}
	if _, kept := pa.Args["image_model"]; kept {
		t.Fatal("the proposal kept the choice the model made")
	}
	if len(pa.Choices) != 1 || pa.Choices[0].Kind != copilot.ChoiceImageModel {
		t.Fatalf("choices %+v", pa.Choices)
	}
}

func TestDriver_AProposalWhoseChoicesCannotBeLoadedIsRefused(t *testing.T) {
	ct := &choiceTool{fakeTool: fakeTool{name: "generate_image", meta: writeMeta}, err: errors.New("catalog down")}
	drv := driverWith(&fakeAccess{}, ct)
	step := drv.Dispatch(context.Background(), ai.ToolCall{Name: "generate_image", Arguments: map[string]interface{}{}}, (&capture{}).emit)
	if step.Pause != nil || !strings.Contains(step.Result, "catalog down") {
		t.Fatalf("step %+v", step)
	}
}

func TestDriver_ApprovalHandsTheChoiceToTheTool(t *testing.T) {
	ct := &choiceTool{fakeTool: fakeTool{name: "generate_image", meta: writeMeta}, choices: imageModelChoice()}
	drv := driverWith(&fakeAccess{}, ct)
	pa := copilot.PendingAction{ToolName: "generate_image", Args: map[string]interface{}{"prompt": "pizza"}, Choices: imageModelChoice()}
	res := drv.ExecuteApproved(context.Background(), pa, copilot.Approval{Choices: map[string]string{"image_model": "openai/gpt-image-2"}}, (&capture{}).emit)
	if res.Status != copilot.StatusOK || ct.gotArgs["image_model"] != "openai/gpt-image-2" {
		t.Fatalf("res=%+v args=%v", res, ct.gotArgs)
	}
}

func TestDriver_ApprovalWithoutTheChoiceChangesNothing(t *testing.T) {
	ct := &choiceTool{fakeTool: fakeTool{name: "generate_image", meta: writeMeta}, choices: imageModelChoice()}
	drv := driverWith(&fakeAccess{}, ct)
	res := drv.ExecuteApproved(context.Background(), copilot.PendingAction{ToolName: "generate_image", Choices: imageModelChoice()}, copilot.Approval{}, (&capture{}).emit)
	if res.Status != copilot.StatusError || ct.calls != 0 {
		t.Fatalf("res=%+v calls=%d", res, ct.calls)
	}
}

func TestDriver_ApprovalAsksTheToolAgainForItsChoices(t *testing.T) {
	ct := &choiceTool{fakeTool: fakeTool{name: "generate_image", meta: writeMeta}, err: errors.New("catalog down")}
	drv := driverWith(&fakeAccess{}, ct)
	res := drv.ExecuteApproved(context.Background(), copilot.PendingAction{ToolName: "generate_image", Choices: imageModelChoice()}, copilot.Approval{Choices: map[string]string{"image_model": "openai/gpt-image-2"}}, (&capture{}).emit)
	if res.Status != copilot.StatusError || ct.calls != 0 {
		t.Fatalf("res=%+v calls=%d", res, ct.calls)
	}
}

func TestDriver_TheChoiceIsCheckedByTheToolBeforeRunning(t *testing.T) {
	ct := &choiceTool{fakeTool: fakeTool{name: "generate_image", meta: writeMeta}, choices: imageModelChoice()}
	ct.invalid = errors.New("model unknown")
	drv := driverWith(&fakeAccess{}, ct)
	res := drv.ExecuteApproved(context.Background(), copilot.PendingAction{ToolName: "generate_image", Choices: imageModelChoice()}, copilot.Approval{Choices: map[string]string{"image_model": "x/invented"}}, (&capture{}).emit)
	if res.Status != copilot.StatusError || ct.calls != 0 {
		t.Fatalf("res=%+v calls=%d", res, ct.calls)
	}
}
