package copilot_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/ai"
	"vozko/domain/aichat"
	"vozko/domain/copilot"
	"vozko/domain/readiness"
	"vozko/usecases/agentloop"
)

type fakeThreads struct {
	thread  *aichat.Thread
	touched bool
	renamed string
}

func (f *fakeThreads) Create(*aichat.Thread) error               { return nil }
func (f *fakeThreads) GetByID(id string) (*aichat.Thread, error) { return f.thread, nil }
func (f *fakeThreads) ListByUser(aichat.ListThreadsInput) ([]*aichat.Thread, int64, error) {
	return nil, 0, nil
}
func (f *fakeThreads) Rename(id, title string) error                { f.renamed = title; return nil }
func (f *fakeThreads) Touch(id string, t time.Time, m string) error { f.touched = true; return nil }
func (f *fakeThreads) Delete(id string) error                       { return nil }

type fakeMessages struct {
	created      []*aichat.Message
	list         []*aichat.Message
	createErr    error
	listErr      error
	listErrAfter int
	listCalls    int
	claimErr     error
}

func (f *fakeMessages) Create(m *aichat.Message) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, m)
	return nil
}
func (f *fakeMessages) ListByThread(in aichat.ListMessagesInput) ([]*aichat.Message, int64, error) {
	f.listCalls++
	if f.listErr != nil && f.listCalls > f.listErrAfter {
		return nil, 0, f.listErr
	}
	return f.list, int64(len(f.list)), nil
}
func (f *fakeMessages) DeleteByThread(string) error { return nil }

func (f *fakeMessages) ClaimProposal(threadID, proposalID string, outcome aichat.ProposalStatus) (*aichat.Message, error) {
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	for _, m := range f.created {
		if m.ThreadID == threadID && m.ProposalID == proposalID && m.ProposalStatus == aichat.ProposalPending {
			m.ProposalStatus = outcome
			return m, nil
		}
	}
	return nil, aichat.ErrProposalNotPending
}

func (f *fakeMessages) ExpireProposals(threadID string) error {
	for _, m := range f.created {
		if m.ThreadID == threadID && m.ProposalStatus == aichat.ProposalPending {
			m.ProposalStatus = aichat.ProposalExpired
		}
	}
	return nil
}

func (f *fakeMessages) propose(pa copilot.PendingAction) *aichat.Message {
	m := &aichat.Message{ThreadID: "t1", Role: aichat.RoleAssistant}
	if err := attachProposal(m, pa); err != nil {
		panic(err)
	}
	f.created = append(f.created, m)
	return m
}

func (f *fakeMessages) proposal(id string) *aichat.Message {
	for _, m := range f.created {
		if m.ProposalID == id {
			return m
		}
	}
	return nil
}

func testThread() *aichat.Thread {
	return &aichat.Thread{ID: "t1", WorkspaceID: "ws1", UserID: "u1", Model: "m"}
}

func newService(prov *scriptAI, th *fakeThreads, ms *fakeMessages, ts ...copilot.Tool) *Service {
	return NewService(agentloop.Engine{AI: prov}, NewRegistry(ts...), &fakeAccess{}, openFunds{}, th, ms, nil, nil, func() string { return "act-1" })
}

func (f *fakeMessages) last() *aichat.Message {
	if len(f.created) == 0 {
		return nil
	}
	return f.created[len(f.created)-1]
}

func TestService_Stream_ReadThenReply(t *testing.T) {
	th := &fakeThreads{thread: testThread()}
	ms := &fakeMessages{}
	rt := &fakeTool{name: "list_agents", meta: readMeta}
	prov := &scriptAI{
		turns: [][]ai.ToolCall{{call("list_agents", nil)}, {}},
		texts: []string{"", "Você tem 3 agentes."},
	}
	svc := newService(prov, th, ms, rt)
	cp := &capture{}
	if err := svc.Stream(context.Background(), th.thread, copilot.UserMessage{Content: "quantos agentes?"}, ownerCtx, cp.emit); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if rt.calls != 1 {
		t.Fatalf("read tool should run, got %d", rt.calls)
	}
	if len(ms.created) != 2 || ms.created[0].Role != aichat.RoleUser || ms.last().Role != aichat.RoleAssistant {
		t.Fatalf("expected user + assistant persisted, got %+v", ms.created)
	}
	if ms.last().Content != "Você tem 3 agentes." {
		t.Fatalf("assistant reply not persisted: %q", ms.last().Content)
	}
	if !cp.has("done") {
		t.Fatal("expected a done event")
	}
	if !th.touched || th.renamed != "quantos agentes?" {
		t.Fatalf("thread should be touched + auto-titled, got touched=%v title=%q", th.touched, th.renamed)
	}
}

func TestService_Stream_MutationAwaitsApproval(t *testing.T) {
	th := &fakeThreads{thread: testThread()}
	ms := &fakeMessages{}
	wt := &fakeTool{name: "create_agent", meta: writeMeta}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("create_agent", map[string]interface{}{"name": "Bot"})}}}
	svc := newService(prov, th, ms, wt)
	cp := &capture{}
	if err := svc.Stream(context.Background(), th.thread, copilot.UserMessage{Content: "crie um agente Bot"}, ownerCtx, cp.emit); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if wt.calls != 0 {
		t.Fatal("mutation must NOT execute before approval")
	}
	if !cp.has("awaiting_approval") {
		t.Fatal("expected an awaiting_approval event")
	}
	last := ms.last()
	if last.Role != aichat.RoleAssistant || last.ProposalID != "act-1" || last.ProposalStatus != aichat.ProposalPending {
		t.Fatalf("the proposal must be stored on the assistant message, got %+v", last)
	}
	var stored copilot.PendingAction
	if err := json.Unmarshal(last.Proposal, &stored); err != nil || stored.ToolName != "create_agent" || stored.Args["name"] != "Bot" {
		t.Fatalf("stored proposal = %+v (%v)", stored, err)
	}
}

func TestService_Stream_MutationWithProposalText(t *testing.T) {
	th := &fakeThreads{thread: testThread()}
	ms := &fakeMessages{}
	wt := &fakeTool{name: "create_agent", meta: writeMeta}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("create_agent", nil)}}, texts: []string{"Vou criar o agente Bot."}}
	svc := newService(prov, th, ms, wt)
	if err := svc.Stream(context.Background(), th.thread, copilot.UserMessage{Content: "crie"}, ownerCtx, (&capture{}).emit); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if ms.last() == nil || ms.last().Content != "Vou criar o agente Bot." {
		t.Fatalf("the model's proposal text should be persisted, got %+v", ms.last())
	}
}

func TestService_Stream_DefaultModelWhenThreadHasNone(t *testing.T) {
	th := &fakeThreads{thread: &aichat.Thread{ID: "t1", WorkspaceID: "ws1", UserID: "u1", Model: ""}}
	ms := &fakeMessages{}
	rt := &fakeTool{name: "list_agents", meta: readMeta}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("list_agents", nil)}, {}}, texts: []string{"", "ok"}}
	svc := newService(prov, th, ms, rt)
	if err := svc.Stream(context.Background(), th.thread, copilot.UserMessage{Content: "oi"}, ownerCtx, (&capture{}).emit); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if ms.last() == nil || ms.last().Model != defaultCopilotModel {
		t.Fatalf("expected the default model when the thread has none, got %+v", ms.last())
	}
}

func TestService_Stream_NoModelOutput(t *testing.T) {
	th := &fakeThreads{thread: testThread()}
	ms := &fakeMessages{}
	svc := newService(&scriptAI{}, th, ms)
	if err := svc.Stream(context.Background(), th.thread, copilot.UserMessage{Content: "oi"}, ownerCtx, (&capture{}).emit); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(ms.created) != 1 || ms.created[0].Role != aichat.RoleUser {
		t.Fatalf("expected only the user message persisted, got %+v", ms.created)
	}
}

func TestService_Stream_Empty(t *testing.T) {
	svc := newService(&scriptAI{}, &fakeThreads{thread: testThread()}, &fakeMessages{})
	if err := svc.Stream(context.Background(), testThread(), copilot.UserMessage{Content: "   "}, ownerCtx, (&capture{}).emit); err != ErrEmptyMessage {
		t.Fatalf("expected ErrEmptyMessage, got %v", err)
	}
}

func TestService_Approve(t *testing.T) {
	th := &fakeThreads{thread: testThread()}
	ms := &fakeMessages{}
	wt := &fakeTool{name: "create_agent", meta: writeMeta}
	prov := &scriptAI{turns: [][]ai.ToolCall{{}}, texts: []string{"Pronto! Criei o agente Bot."}}
	svc := newService(prov, th, ms, wt)
	ms.propose(copilot.PendingAction{ID: "act-1", ToolName: "create_agent", Args: map[string]interface{}{"name": "Bot"}})

	cp := &capture{}
	if err := svc.Approve(context.Background(), th.thread, "act-1", ownerCtx, cp.emit); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if wt.calls != 1 || wt.gotCC.WorkspaceID != "ws1" {
		t.Fatalf("approval must run the tool scoped, got calls=%d cc=%+v", wt.calls, wt.gotCC)
	}
	if ms.proposal("act-1").ProposalStatus != aichat.ProposalApproved {
		t.Fatalf("the proposal must be marked approved, got %q", ms.proposal("act-1").ProposalStatus)
	}
	if err := svc.Approve(context.Background(), th.thread, "act-1", ownerCtx, cp.emit); err != ErrActionNotFound || wt.calls != 1 {
		t.Fatalf("a second approval must not run the tool again, got %v calls=%d", err, wt.calls)
	}
	if ms.last() == nil || !strings.Contains(ms.last().Content, "Criei") {
		t.Fatalf("the model's continuation must be persisted, got %+v", ms.last())
	}
	if !cp.has("tool") || !cp.has("done") || !th.touched {
		t.Fatal("expected executed-tool + done events and a thread touch")
	}
}

func TestService_Approve_DefaultModel(t *testing.T) {
	th := &fakeThreads{thread: &aichat.Thread{ID: "t1", WorkspaceID: "ws1", UserID: "u1", Model: ""}}
	ms := &fakeMessages{}
	wt := &fakeTool{name: "create_agent", meta: writeMeta}
	prov := &scriptAI{turns: [][]ai.ToolCall{{}}, texts: []string{"ok"}}
	svc := newService(prov, th, ms, wt)
	ms.propose(copilot.PendingAction{ID: "act-1", ToolName: "create_agent"})
	if err := svc.Approve(context.Background(), th.thread, "act-1", ownerCtx, (&capture{}).emit); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if ms.last() == nil || ms.last().Model != defaultCopilotModel {
		t.Fatalf("approval should fall back to the default model, got %+v", ms.last())
	}
}

func TestService_Approve_NotFound(t *testing.T) {
	svc := newService(&scriptAI{}, &fakeThreads{thread: testThread()}, &fakeMessages{})
	if err := svc.Approve(context.Background(), testThread(), "nope", ownerCtx, (&capture{}).emit); err != ErrActionNotFound {
		t.Fatalf("expected ErrActionNotFound, got %v", err)
	}
}

func TestService_Reject(t *testing.T) {
	th := &fakeThreads{thread: testThread()}
	ms := &fakeMessages{}
	wt := &fakeTool{name: "create_agent", meta: writeMeta}
	svc := newService(&scriptAI{}, th, ms, wt)
	ms.propose(copilot.PendingAction{ID: "act-1", ToolName: "create_agent"})

	cp := &capture{}
	if err := svc.Reject(context.Background(), th.thread, "act-1", cp.emit); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if wt.calls != 0 {
		t.Fatal("reject must NOT run the tool")
	}
	if ms.proposal("act-1").ProposalStatus != aichat.ProposalRejected {
		t.Fatalf("the proposal must be marked rejected, got %q", ms.proposal("act-1").ProposalStatus)
	}
	if ms.last() == nil || !strings.Contains(ms.last().Content, "cancelada") {
		t.Fatalf("a cancellation message must be persisted, got %+v", ms.last())
	}
	if !cp.has("done") {
		t.Fatal("expected done event")
	}
}

func TestService_Reject_NotFound(t *testing.T) {
	svc := newService(&scriptAI{}, &fakeThreads{thread: testThread()}, &fakeMessages{})
	if err := svc.Reject(context.Background(), testThread(), "nope", (&capture{}).emit); err != ErrActionNotFound {
		t.Fatalf("expected ErrActionNotFound, got %v", err)
	}
}

func TestService_BuildHistorySkipsToolTurns(t *testing.T) {
	ms := &fakeMessages{list: []*aichat.Message{
		{Role: aichat.RoleUser, Content: "oi"},
		{Role: aichat.RoleAssistant, Content: "olá"},
		{Role: aichat.RoleSystem, Content: "sys"},
		{Role: aichat.RoleTool, Content: "tool-result"},
	}}
	svc := newService(&scriptAI{}, &fakeThreads{thread: testThread()}, ms)
	h, err := svc.buildHistory("t1")
	if err != nil {
		t.Fatalf("buildHistory: %v", err)
	}
	if len(h) != 3 {
		t.Fatalf("tool turns must be skipped, got %d: %+v", len(h), h)
	}
	if h[0].Role != ai.RoleUser || h[1].Role != ai.RoleAssistant || h[2].Role != ai.RoleSystem {
		t.Fatalf("unexpected role mapping: %+v", h)
	}
}

func TestService_Stream_HistoryError(t *testing.T) {
	ms := &fakeMessages{listErr: errors.New("db")}
	svc := newService(&scriptAI{}, &fakeThreads{thread: testThread()}, ms)
	if err := svc.Stream(context.Background(), testThread(), copilot.UserMessage{Content: "x"}, ownerCtx, (&capture{}).emit); err == nil {
		t.Fatal("expected history error to propagate")
	}
}

func TestService_BuildHistory_SecondListError(t *testing.T) {
	ms := &fakeMessages{listErr: errors.New("db"), listErrAfter: 1}
	svc := newService(&scriptAI{}, &fakeThreads{thread: testThread()}, ms)
	if _, err := svc.buildHistory("t1"); err == nil {
		t.Fatal("expected the second list error")
	}
}

func TestService_BuildHistory_Offset(t *testing.T) {
	list := make([]*aichat.Message, 41)
	for i := range list {
		list[i] = &aichat.Message{Role: aichat.RoleUser, Content: "m"}
	}
	svc := newService(&scriptAI{}, &fakeThreads{thread: testThread()}, &fakeMessages{list: list})
	if _, err := svc.buildHistory("t1"); err != nil {
		t.Fatalf("buildHistory with offset: %v", err)
	}
}

func TestService_Stream_UserCreateError(t *testing.T) {
	ms := &fakeMessages{createErr: errors.New("db")}
	svc := newService(&scriptAI{}, &fakeThreads{thread: testThread()}, ms)
	if err := svc.Stream(context.Background(), testThread(), copilot.UserMessage{Content: "x"}, ownerCtx, (&capture{}).emit); err == nil {
		t.Fatal("expected user-create error to propagate")
	}
}

func TestService_Stream_PendingSaveError(t *testing.T) {
	wt := &fakeTool{name: "create_agent", meta: writeMeta}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("create_agent", nil)}}}
	ms := &fakeMessages{}
	svc := newService(prov, &fakeThreads{thread: testThread()}, ms, wt)
	ms.createErr = errors.New("db")
	if err := svc.Stream(context.Background(), testThread(), copilot.UserMessage{Content: "crie"}, ownerCtx, (&capture{}).emit); err == nil {
		t.Fatal("expected the proposal save error to propagate")
	}
}

func TestService_Approve_GetError(t *testing.T) {
	svc := newService(&scriptAI{}, &fakeThreads{thread: testThread()}, &fakeMessages{claimErr: errors.New("db")})
	if err := svc.Approve(context.Background(), testThread(), "a", ownerCtx, (&capture{}).emit); err == nil {
		t.Fatal("expected approve get error")
	}
}

func TestService_Reject_GetError(t *testing.T) {
	svc := newService(&scriptAI{}, &fakeThreads{thread: testThread()}, &fakeMessages{claimErr: errors.New("db")})
	if err := svc.Reject(context.Background(), testThread(), "a", (&capture{}).emit); err == nil {
		t.Fatal("expected reject get error")
	}
}

func TestTurnRecorder(t *testing.T) {
	var seen []string
	rec := &turnRecorder{emit: func(tp string, _ interface{}) { seen = append(seen, tp) }}
	rec.emitFn("reasoning_delta", map[string]string{"text": "hmm "})
	rec.emitFn("reasoning_delta", map[string]interface{}{"text": "more"})
	rec.emitFn("reasoning_delta", 123)
	rec.emitFn("tool", map[string]interface{}{"name": "list_models", "summary": "ok", "ok": true})
	rec.emitFn("assistant_delta", map[string]string{"text": "hi"})
	if len(seen) != 5 {
		t.Fatalf("every event must pass through, got %v", seen)
	}

	m := rec.message("t1", "answer", "model")
	if m.Content != "answer" || m.Role != aichat.RoleAssistant {
		t.Fatalf("message basics: %+v", m)
	}
	var reasoning string
	if json.Unmarshal(m.Reasoning, &reasoning) != nil || reasoning != "hmm more" {
		t.Fatalf("reasoning accumulated: %q", reasoning)
	}
	var tools []toolStep
	if json.Unmarshal(m.ToolCalls, &tools) != nil || len(tools) != 1 || tools[0].Name != "list_models" || !tools[0].Ok {
		t.Fatalf("tools accumulated: %+v", tools)
	}

	empty := (&turnRecorder{emit: func(string, interface{}) {}}).message("t1", "x", "m")
	if empty.Reasoning != nil || empty.ToolCalls != nil {
		t.Fatal("empty recorder leaves reasoning/tools nil")
	}
}

func TestServiceHelpers(t *testing.T) {
	long := strings.Repeat("a", 80)
	if title := deriveTitle(long); len([]rune(title)) > maxTitleLen+1 || !strings.HasSuffix(title, "…") {
		t.Fatalf("deriveTitle should truncate, got len=%d", len([]rune(title)))
	}
	if deriveTitle("curto") != "curto" {
		t.Fatal("short titles unchanged")
	}
	if lastAssistantContent([]ai.Message{{Role: ai.RoleUser, Content: "x"}}) != "" {
		t.Fatal("no assistant → empty")
	}
	if lastAssistantContent([]ai.Message{{Role: ai.RoleAssistant, Content: "a"}, {Role: ai.RoleAssistant, Content: "b"}}) != "b" {
		t.Fatal("last non-empty assistant")
	}

	ok := approvalContinuationPrompt(copilot.PendingAction{ToolName: "create_agent", Summary: "cria Bot"}, copilot.Result{Status: copilot.StatusOK, Data: map[string]string{"id": "a1"}})
	if !strings.Contains(ok, "SUCESSO") || !strings.Contains(ok, "cria Bot") || !strings.Contains(ok, "a1") {
		t.Fatalf("ok continuation should mention success, summary and data: %q", ok)
	}
	if !strings.Contains(approvalContinuationPrompt(copilot.PendingAction{ToolName: "x"}, copilot.Result{Status: copilot.StatusDenied}), "permissão") {
		t.Fatal("denied continuation")
	}
	fail := approvalContinuationPrompt(copilot.PendingAction{ToolName: "x"}, copilot.Result{Status: copilot.StatusError, Message: "ruim"})
	if !strings.Contains(fail, "FALHOU") || !strings.Contains(fail, "ruim") {
		t.Fatal("fail continuation includes the error message")
	}
	if renderData(nil) != "" {
		t.Fatal("nil data → empty")
	}
	if renderData(map[string]string{"a": "b"}) == "" {
		t.Fatal("data should marshal")
	}
	if renderData(make(chan int)) != "" {
		t.Fatal("unmarshalable data → empty")
	}
}

func TestService_NewMessageExpiresAnOpenProposal(t *testing.T) {
	th := &fakeThreads{thread: testThread()}
	ms := &fakeMessages{}
	wt := &fakeTool{name: "create_agent", meta: writeMeta}
	svc := newService(&scriptAI{}, th, ms, wt)
	ms.propose(copilot.PendingAction{ID: "act-1", ToolName: "create_agent"})
	if err := svc.Stream(context.Background(), th.thread, copilot.UserMessage{Content: "na verdade, deixa pra lá"}, ownerCtx, (&capture{}).emit); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if ms.proposal("act-1").ProposalStatus != aichat.ProposalExpired {
		t.Fatalf("an unanswered proposal must expire when the user moves on, got %q", ms.proposal("act-1").ProposalStatus)
	}
	if err := svc.Approve(context.Background(), th.thread, "act-1", ownerCtx, (&capture{}).emit); err != ErrActionNotFound || wt.calls != 0 {
		t.Fatalf("an expired proposal must not run, got %v calls=%d", err, wt.calls)
	}
}

type previewTool struct{ *fakeTool }

func (p previewTool) Preview(_ context.Context, cc copilot.Context, _ map[string]interface{}) *copilot.Preview {
	return &copilot.Preview{Kind: copilot.PreviewWhatsAppTemplate, Data: map[string]interface{}{"workspace": cc.WorkspaceID}}
}

func TestService_ProposalCarriesTheToolPreviewThroughStorage(t *testing.T) {
	th := &fakeThreads{thread: testThread()}
	ms := &fakeMessages{}
	wt := previewTool{&fakeTool{name: "create_template", meta: writeMeta}}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("create_template", nil)}}}
	if err := newService(prov, th, ms, wt).Stream(context.Background(), th.thread, copilot.UserMessage{Content: "crie"}, ownerCtx, (&capture{}).emit); err != nil {
		t.Fatalf("stream: %v", err)
	}
	var stored copilot.PendingAction
	if err := json.Unmarshal(ms.last().Proposal, &stored); err != nil || stored.Preview == nil || stored.Preview.Kind != copilot.PreviewWhatsAppTemplate {
		t.Fatalf("stored preview = %+v (%v)", stored.Preview, err)
	}
}

type fixedState struct {
	snap   *readiness.Snapshot
	person readiness.Person
}

func (f *fixedState) Snapshot(_ context.Context, p readiness.Person) (*readiness.Snapshot, error) {
	f.person = p
	return f.snap, nil
}

func TestService_TheModelSeesTheWorkspaceStateEveryTurn(t *testing.T) {
	th := &fakeThreads{thread: testThread()}
	prov := &scriptAI{}
	state := &fixedState{snap: &readiness.Snapshot{SubscriptionActive: true, Capabilities: []readiness.Status{
		{Capability: readiness.OfficialWhatsApp, Usage: &readiness.Usage{Used: 0, Total: 1}, CanAdd: true},
	}}}
	svc := NewService(agentloop.Engine{AI: prov}, NewRegistry(), &fakeAccess{}, openFunds{}, th, &fakeMessages{}, nil, state, func() string { return "a" })
	if err := svc.Stream(context.Background(), th.thread, copilot.UserMessage{Content: "crie um modelo"}, ownerCtx, (&capture{}).emit); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(prov.inputs) == 0 || !strings.Contains(prov.inputs[0].SystemPrompt, "WhatsApp oficial: 0 (0 de 1 do plano); pode adicionar") {
		t.Fatal("the system prompt must carry the live workspace state")
	}
	if state.person.WorkspaceID != ownerCtx.WorkspaceID || state.person.UserID != ownerCtx.UserID {
		t.Fatalf("state read for %+v", state.person)
	}
}
