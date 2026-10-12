package copilot_usecase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/ai"
	"vozko/domain/aichat"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/usecases/agentloop"
)

type fakeQueue struct {
	mu       sync.Mutex
	messages *fakeMessages
	rows     []*aichat.QueuedMessage
	failWith error
}

func (q *fakeQueue) Enqueue(m *aichat.QueuedMessage) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, row := range q.rows {
		if row.ThreadID == m.ThreadID && row.ClientID == m.ClientID {
			*m = *row
			return nil
		}
	}
	m.ID, m.CreatedAt = "q-"+m.ClientID, time.Now()
	stored := *m
	q.rows = append(q.rows, &stored)
	return nil
}

func (q *fakeQueue) Pending(threadID string) ([]*aichat.QueuedMessage, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.failWith != nil {
		return nil, q.failWith
	}
	var out []*aichat.QueuedMessage
	for _, row := range q.rows {
		if row.ThreadID == threadID && !row.Delivered() {
			copied := *row
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (q *fakeQueue) Remove(threadID, clientID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, row := range q.rows {
		if row.ThreadID == threadID && row.ClientID == clientID && !row.Delivered() {
			q.rows = append(q.rows[:i], q.rows[i+1:]...)
			return nil
		}
	}
	return aichat.ErrQueuedMessageGone
}

func (q *fakeQueue) Deliver(threadID string) ([]*aichat.QueuedMessage, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.failWith != nil {
		return nil, q.failWith
	}
	var out []*aichat.QueuedMessage
	for _, row := range q.rows {
		if row.ThreadID != threadID || row.Delivered() {
			continue
		}
		m := row.Message()
		m.ID = "m-" + row.ClientID
		if err := q.messages.Create(m); err != nil {
			return nil, err
		}
		now := time.Now()
		row.MessageID, row.DeliveredAt = m.ID, &now
		copied := *row
		out = append(out, &copied)
	}
	return out, nil
}

type hookAI struct {
	*scriptAI
	before func(call int)
}

func (h *hookAI) GenerateStream(ctx context.Context, in ai.GenerateInput) (<-chan ai.StreamEvent, error) {
	if h.before != nil {
		h.before(h.idx)
	}
	return h.scriptAI.GenerateStream(ctx, in)
}

type hookTool struct {
	name string
	run  func()
}

func (h *hookTool) Definition() tools.Definition {
	return tools.Definition{Name: h.name, Description: "x"}
}
func (h *hookTool) Meta() copilot.Meta { return readMeta }
func (h *hookTool) Execute(context.Context, copilot.Context, map[string]interface{}) copilot.Result {
	if h.run != nil {
		h.run()
	}
	return copilot.Result{Status: copilot.StatusOK, Data: "lido"}
}

type queueRig struct {
	svc   *Service
	th    *fakeThreads
	ms    *fakeMessages
	queue *fakeQueue
	prov  *scriptAI
}

func newQueueRig(prov ai.Service, script *scriptAI, funds FundsChecker, ts ...copilot.Tool) *queueRig {
	th, ms := &fakeThreads{thread: testThread()}, &fakeMessages{}
	queue := &fakeQueue{messages: ms}
	svc := NewService(agentloop.Engine{AI: prov}, NewRegistry(ts...), &fakeAccess{}, funds, th, ms, nil, nil, func() string { return "act-1" })
	svc.SetQueue(queue)
	return &queueRig{svc: svc, th: th, ms: ms, queue: queue, prov: script}
}

func (r *queueRig) answer(t *testing.T, run TurnFunc) []copilot.TurnEvent {
	t.Helper()
	turn, err := r.svc.BeginTurn(context.Background(), r.th.thread.ID, "u1")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	_, live, leave := turn.Subscribe()
	defer leave()
	r.svc.Start(turn, r.th.thread, ownerCtx, run)
	return waitClosed(t, live)
}

func (r *queueRig) send(content string) TurnFunc {
	return func(ctx context.Context, emit agentloop.Emit) error {
		return r.svc.Stream(ctx, r.th.thread, copilot.UserMessage{Content: content}, ownerCtx, emit)
	}
}

func (r *queueRig) enqueue(t *testing.T, clientID, content string) {
	t.Helper()
	if _, err := r.svc.Enqueue(r.th.thread, "u1", copilot.UserMessage{ClientID: clientID, Content: content}); err != nil {
		t.Errorf("enqueue %s: %v", clientID, err)
	}
}

func sees(in ai.GenerateInput, text string) bool {
	for _, m := range in.Messages {
		if m.Role == ai.RoleUser && strings.Contains(m.Content, text) {
			return true
		}
	}
	return false
}

func roles(ms []*aichat.Message) string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = string(m.Role)
	}
	return strings.Join(out, ",")
}

func assertChargedOnceEach(t *testing.T, prov *scriptAI, want int) {
	t.Helper()
	if len(prov.inputs) != want {
		t.Fatalf("model calls = %d, want %d: every call runs and is charged exactly once", len(prov.inputs), want)
	}
	for i, in := range prov.inputs {
		if in.BillingReference != aichat.ChargeReference("t1") || in.SessionID != aichat.ChargeReference("t1") {
			t.Fatalf("call %d is not charged to the thread: %q", i, in.BillingReference)
		}
	}
}

func TestAMessageSentDuringAToolReachesTheNextModelCallOfTheSameTurn(t *testing.T) {
	var rig *queueRig
	release := make(chan struct{})
	waiting := &hookTool{name: "studio_read", run: func() {
		rig.enqueue(t, "c1", "use o logo azul")
		close(release)
	}}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("studio_read", nil)}, {}}, texts: []string{"Vou olhar a tela.", "Feito, com o logo azul."}}
	rig = newQueueRig(prov, prov, openFunds{}, waiting)
	evs := rig.answer(t, rig.send("monte o anúncio"))
	<-release

	assertChargedOnceEach(t, prov, 2)
	if sees(prov.inputs[0], "logo azul") {
		t.Fatal("the first call ran before the message existed")
	}
	if !sees(prov.inputs[1], agentloop.Interjection("use o logo azul")) {
		t.Fatal("the next call must see the new message")
	}
	if got := roles(rig.ms.created); got != "user,assistant,user,assistant" {
		t.Fatalf("thread = %s, want the reply split around the delivered message", got)
	}
	if rig.ms.created[1].Content != "Vou olhar a tela." || len(rig.ms.created[1].ToolCalls) == 0 {
		t.Fatalf("the part before the message keeps its text and tools: %+v", rig.ms.created[1])
	}
	if rig.ms.created[2].Content != "use o logo azul" || rig.ms.last().Content != "Feito, com o logo azul." {
		t.Fatalf("thread = %+v", rig.ms.created)
	}
	if strings.Contains(string(rig.ms.last().ToolCalls), "studio_read") {
		t.Fatal("a tool must be saved once, with the part it belongs to")
	}
	if got := types(evs); !contains(got, EventDelivered) || !contains(got, "done") {
		t.Fatalf("events = %v", got)
	}
	if pending, _ := rig.queue.Pending("t1"); len(pending) != 0 {
		t.Fatalf("queue = %+v", pending)
	}
}

func TestAMessageSentDuringTheFinalAnswerStartsOneFollowUpTurn(t *testing.T) {
	script := &scriptAI{turns: [][]ai.ToolCall{{}, {}}, texts: []string{"Aqui está.", "Sobre a segunda pergunta: sim."}}
	var rig *queueRig
	prov := &hookAI{scriptAI: script, before: func(call int) {
		if call == 0 {
			rig.enqueue(t, "c1", "e a segunda pergunta?")
		}
	}}
	rig = newQueueRig(prov, script, openFunds{}, &fakeTool{name: "noop", meta: readMeta})
	evs := rig.answer(t, rig.send("primeira pergunta"))

	assertChargedOnceEach(t, script, 2)
	if sees(script.inputs[0], "segunda pergunta") {
		t.Fatal("the running call must not be re-sent with the new message")
	}
	if !sees(script.inputs[1], agentloop.UserRequest("e a segunda pergunta?")) {
		t.Fatal("the follow-up answers the queued message as a request")
	}
	if got := roles(rig.ms.created); got != "user,assistant,user,assistant" {
		t.Fatalf("thread = %s", got)
	}
	got := types(evs)
	if count(got, "done") != 2 || count(got, EventDelivered) != 1 {
		t.Fatalf("events = %v, want two answers and one delivery", got)
	}
}

func TestAMessageQueuedWhileAnApprovalWaitsIsDeliveredByTheApprovedTurn(t *testing.T) {
	prov := &scriptAI{turns: [][]ai.ToolCall{{}}, texts: []string{"Criado, e já ajustei o nome."}}
	wt := &fakeTool{name: "create_agent", meta: writeMeta}
	rig := newQueueRig(prov, prov, openFunds{}, wt)
	rig.ms.propose(copilot.PendingAction{ID: "act-1", ToolName: "create_agent", Args: map[string]interface{}{"name": "Bot"}})
	rig.enqueue(t, "c1", "chame de Atendente")
	if rig.svc.TurnRunning("t1") {
		t.Fatal("queueing must never start a turn on its own")
	}
	rig.answer(t, func(ctx context.Context, emit agentloop.Emit) error {
		return rig.svc.Approve(ctx, rig.th.thread, "act-1", copilot.Approval{}, ownerCtx, emit)
	})
	assertChargedOnceEach(t, prov, 1)
	if !sees(prov.inputs[0], agentloop.Interjection("chame de Atendente")) {
		t.Fatal("the approved turn must see the queued message before its call")
	}
	if wt.calls != 1 {
		t.Fatalf("approved tool ran %d times", wt.calls)
	}
}

func TestARejectionAnswersTheQueuedMessage(t *testing.T) {
	prov := &scriptAI{turns: [][]ai.ToolCall{{}}, texts: []string{"Certo, não criei."}}
	rig := newQueueRig(prov, prov, openFunds{}, &fakeTool{name: "create_agent", meta: writeMeta})
	rig.ms.propose(copilot.PendingAction{ID: "act-1", ToolName: "create_agent"})
	rig.enqueue(t, "c1", "deixa pra lá")
	evs := rig.answer(t, func(ctx context.Context, emit agentloop.Emit) error {
		return rig.svc.Reject(ctx, rig.th.thread, "act-1", emit)
	})
	assertChargedOnceEach(t, prov, 1)
	if !sees(prov.inputs[0], "deixa pra lá") || count(types(evs), "done") != 2 {
		t.Fatalf("events %v", types(evs))
	}
}

func TestAStoppedTurnLeavesTheQueueForTheNextSend(t *testing.T) {
	var rig *queueRig
	stopper := &hookTool{name: "read_x", run: func() {
		rig.enqueue(t, "c1", "na verdade, pare")
		if err := rig.svc.StopTurn("t1", "u1"); err != nil {
			t.Errorf("stop: %v", err)
		}
	}}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("read_x", nil)}, {}, {}}, texts: []string{"", "nunca", "Ok, parei."}}
	rig = newQueueRig(prov, prov, openFunds{}, stopper)
	rig.answer(t, rig.send("faça tudo"))
	assertChargedOnceEach(t, prov, 1)
	if pending, _ := rig.queue.Pending("t1"); len(pending) != 1 {
		t.Fatalf("stop must keep the queue, got %+v", pending)
	}

	rig.answer(t, rig.send(""))
	assertChargedOnceEach(t, prov, 2)
	if !sees(prov.inputs[1], agentloop.UserRequest("na verdade, pare")) {
		t.Fatal("sending with an empty composer delivers the queue")
	}
	if pending, _ := rig.queue.Pending("t1"); len(pending) != 0 {
		t.Fatalf("queue = %+v", pending)
	}
}

func TestTheNextSendCarriesTheQueueBeforeItsOwnMessage(t *testing.T) {
	prov := &scriptAI{turns: [][]ai.ToolCall{{}}, texts: []string{"ok"}}
	rig := newQueueRig(prov, prov, openFunds{}, &fakeTool{name: "noop", meta: readMeta})
	rig.queue.rows = []*aichat.QueuedMessage{{ThreadID: "t1", ClientID: "c1", Content: "antes"}}
	rig.answer(t, rig.send("depois"))
	assertChargedOnceEach(t, prov, 1)
	if got := roles(rig.ms.created); got != "user,user,assistant" || rig.ms.created[0].Content != "antes" || rig.ms.created[1].Content != "depois" {
		t.Fatalf("thread = %s %+v", got, rig.ms.created)
	}
	if !sees(prov.inputs[0], "antes") {
		t.Fatal("the model must see the queued message")
	}
}

func TestAnIdleThreadRefusesToQueue(t *testing.T) {
	prov := &scriptAI{}
	rig := newQueueRig(prov, prov, openFunds{})
	_, err := rig.svc.Enqueue(rig.th.thread, "u1", copilot.UserMessage{ClientID: "c1", Content: "oi"})
	if !errors.Is(err, copilot.ErrTurnNotRunning) || len(rig.queue.rows) != 0 {
		t.Fatalf("err %v rows %d", err, len(rig.queue.rows))
	}
}

func TestQueueingValidatesLikeANormalMessage(t *testing.T) {
	prov := &scriptAI{}
	rig := newQueueRig(prov, prov, openFunds{})
	rig.ms.propose(copilot.PendingAction{ID: "act-1", ToolName: "x"})
	if _, err := rig.svc.Enqueue(rig.th.thread, "u1", copilot.UserMessage{ClientID: "bad id", Content: "oi"}); !errors.Is(err, aichat.ErrInvalidClientID) {
		t.Fatalf("bad client id: %v", err)
	}
	if _, err := rig.svc.Enqueue(rig.th.thread, "u1", copilot.UserMessage{ClientID: "c1", Content: "  "}); !errors.Is(err, ErrEmptyMessage) {
		t.Fatalf("empty: %v", err)
	}
	if _, err := rig.svc.Enqueue(rig.th.thread, "u1", copilot.UserMessage{ClientID: "c1", Content: "x", AttachmentIDs: []string{"m1"}}); !errors.Is(err, copilot.ErrAttachmentNotFound) {
		t.Fatalf("unknown attachment: %v", err)
	}
	if len(rig.queue.rows) != 0 {
		t.Fatalf("rows = %d", len(rig.queue.rows))
	}
}

func TestQueueingFailsClosedWithoutAQueue(t *testing.T) {
	th, ms := &fakeThreads{thread: testThread()}, &fakeMessages{}
	svc := NewService(agentloop.Engine{AI: &scriptAI{}}, NewRegistry(), &fakeAccess{}, openFunds{}, th, ms, nil, nil, func() string { return "a" })
	if _, err := svc.Enqueue(th.thread, "u1", copilot.UserMessage{ClientID: "c1", Content: "oi"}); !errors.Is(err, ErrQueueUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestARetriedQueueSendIsDeliveredOnce(t *testing.T) {
	var rig *queueRig
	tool := &hookTool{name: "read_x", run: func() {
		rig.enqueue(t, "c1", "uma vez")
		rig.enqueue(t, "c1", "uma vez")
	}}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("read_x", nil)}, {}}, texts: []string{"", "ok"}}
	rig = newQueueRig(prov, prov, openFunds{}, tool)
	rig.answer(t, rig.send("x"))
	assertChargedOnceEach(t, prov, 2)
	n := 0
	for _, m := range prov.inputs[1].Messages {
		if strings.Contains(m.Content, "uma vez") {
			n++
		}
	}
	if n != 1 || len(rig.queue.rows) != 1 {
		t.Fatalf("seen %d times, rows %d", n, len(rig.queue.rows))
	}
}

func TestARemovedMessageNeverReachesTheModel(t *testing.T) {
	var rig *queueRig
	tool := &hookTool{name: "read_x", run: func() {
		rig.enqueue(t, "c1", "esquece")
		if err := rig.svc.Unqueue(rig.th.thread, "u1", "c1"); err != nil {
			t.Errorf("unqueue: %v", err)
		}
	}}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("read_x", nil)}, {}}, texts: []string{"", "ok"}}
	rig = newQueueRig(prov, prov, openFunds{}, tool)
	evs := rig.answer(t, rig.send("x"))
	assertChargedOnceEach(t, prov, 2)
	if sees(prov.inputs[1], "esquece") {
		t.Fatal("a removed message reached the model")
	}
	got := types(evs)
	if !contains(got, EventQueued) || !contains(got, EventUnqueued) || contains(got, EventDelivered) {
		t.Fatalf("events = %v", got)
	}
	if err := rig.svc.Unqueue(rig.th.thread, "u1", "c1"); !errors.Is(err, aichat.ErrQueuedMessageGone) {
		t.Fatalf("second removal: %v", err)
	}
}

func TestAFollowUpStillAsksTheBalanceBeforeItsCall(t *testing.T) {
	script := &scriptAI{turns: [][]ai.ToolCall{{}, {}}, texts: []string{"primeira", "nunca"}}
	var rig *queueRig
	prov := &hookAI{scriptAI: script, before: func(call int) {
		if call == 0 {
			rig.enqueue(t, "c1", "mais uma")
		}
	}}
	rig = newQueueRig(prov, script, &drainingFunds{allowed: 1}, &fakeTool{name: "noop", meta: readMeta})
	rig.answer(t, rig.send("x"))
	assertChargedOnceEach(t, script, 1)
	if pending, _ := rig.queue.Pending("t1"); len(pending) != 0 {
		t.Fatalf("the follow-up delivers the message even when it cannot answer: %+v", pending)
	}
}

func TestASendWhileATurnRunsNeverStartsASecondTurn(t *testing.T) {
	var rig *queueRig
	tool := &hookTool{name: "read_x", run: func() {
		if _, err := rig.svc.BeginTurn(context.Background(), "t1", "u1"); !errors.Is(err, copilot.ErrTurnRunning) {
			t.Errorf("a second turn began: %v", err)
		}
	}}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("read_x", nil)}, {}}, texts: []string{"", "ok"}}
	rig = newQueueRig(prov, prov, openFunds{}, tool)
	rig.answer(t, rig.send("x"))
	assertChargedOnceEach(t, prov, 2)
}

func TestABrokenQueueNeverStartsAFollowUp(t *testing.T) {
	prov := &scriptAI{turns: [][]ai.ToolCall{{}}, texts: []string{"ok"}}
	rig := newQueueRig(prov, prov, openFunds{}, &fakeTool{name: "noop", meta: readMeta})
	rig.queue.rows = []*aichat.QueuedMessage{{ThreadID: "t1", ClientID: "c1", Content: "x"}}
	rig.queue.failWith = errors.New("db down")
	rig.answer(t, func(context.Context, agentloop.Emit) error { return nil })
	if len(prov.inputs) != 0 {
		t.Fatalf("model calls = %d", len(prov.inputs))
	}
}

func contains(list []string, want string) bool {
	return count(list, want) > 0
}

func count(list []string, want string) int {
	n := 0
	for _, x := range list {
		if x == want {
			n++
		}
	}
	return n
}
