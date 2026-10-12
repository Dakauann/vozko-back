package copilot_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/ai"
	"vozko/domain/aichat"
	"vozko/domain/copilot"
	"vozko/usecases/agentloop"
)

var createdView = copilot.View{Surface: copilot.SurfaceStudio, ProjectID: "6a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d", ProjectKind: copilot.StudioImage}

type fakeAdopter struct {
	mu      sync.Mutex
	adopted map[string]string
	err     error
}

func (f *fakeAdopter) AdoptProject(threadID, projectID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.adopted[threadID] = projectID
	return nil
}

func (f *fakeAdopter) of(threadID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.adopted[threadID]
}

func projectCreator(opened bool) *fakeTool {
	return &fakeTool{name: "new_studio_project", meta: readMeta, result: copilot.Result{
		Status:  copilot.StatusOK,
		Data:    map[string]string{"state": "criado"},
		Opening: &copilot.ProjectOpening{View: createdView, Opened: opened},
	}}
}

type openingRig struct {
	*queueRig
	adopter *fakeAdopter
}

func newOpeningRig(prov ai.Service, script *scriptAI, ts ...copilot.Tool) *openingRig {
	rig := &openingRig{queueRig: newQueueRig(prov, script, openFunds{}, ts...), adopter: &fakeAdopter{adopted: map[string]string{}}}
	rig.ms.listCreated = true
	rig.svc.SetScreenMailbox(newMailbox())
	rig.svc.SetProjectAdopter(rig.adopter)
	return rig
}

func studioCtx(view copilot.View) copilot.Context {
	cc := ownerCtx
	cc.View = view
	return cc
}

func offers(in ai.GenerateInput, name string) bool {
	for _, def := range in.Tools {
		if def.Name == name {
			return true
		}
	}
	return false
}

func (r *openingRig) awaitingThread() {
	steps, _ := json.Marshal([]toolStep{{Name: "new_studio_project", Summary: "ok", Ok: true, Awaits: &createdView}})
	r.th.thread.ProjectID = createdView.ProjectID
	r.ms.created = append(r.ms.created,
		&aichat.Message{ThreadID: "t1", Role: aichat.RoleUser, Content: "crie a capa"},
		&aichat.Message{ThreadID: "t1", Role: aichat.RoleAssistant, Content: "Toque em Abrir no Estúdio no cartão.", ToolCalls: steps})
}

func (r *openingRig) follow(t *testing.T) []copilot.TurnEvent {
	t.Helper()
	turn, err := r.svc.ObserveTurn("t1", "u1")
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	replay, live, leave := turn.Subscribe()
	defer leave()
	return append(replay, waitClosed(t, live)...)
}

func TestAnOpenedEditorGivesTheSameAnswerTheStudioWithoutAnotherMessage(t *testing.T) {
	read := newStudioTool("studio_read", readMeta)
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("new_studio_project", nil)}, {call("studio_read", nil)}, {}}, texts: []string{"", "", "Comecei pela capa."}}
	rig := newOpeningRig(prov, prov, projectCreator(true), read)
	rig.answer(t, rig.send("crie a capa do carrossel"))

	assertChargedOnceEach(t, prov, 3)
	if offers(prov.inputs[0], "studio_read") || !offers(prov.inputs[1], "studio_read") {
		t.Fatal("the studio tools arrive in the call right after the editor opened")
	}
	if !strings.Contains(prov.inputs[1].SystemPrompt, "# Estúdio") || offers(prov.inputs[1], "new_studio_project") {
		t.Fatal("from then on the answer runs on the studio screen of the new project")
	}
	screen, ok := read.gotCC.Screen.(*screenSession)
	if read.calls != 1 || read.gotCC.View != createdView || !ok || screen.projectID != createdView.ProjectID {
		t.Fatalf("the studio tool must act on the created project, got %+v", read.gotCC)
	}
	if rig.adopter.of("t1") != createdView.ProjectID || rig.th.thread.ProjectID != createdView.ProjectID {
		t.Fatal("the thread must become the session of the project she created")
	}
	if strings.Contains(string(rig.ms.last().ToolCalls), "awaits") {
		t.Fatal("an open editor is not waited for")
	}
}

func TestAClosedEditorKeepsTheAnswerWhereItIsAndIsRemembered(t *testing.T) {
	read := newStudioTool("studio_read", readMeta)
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("new_studio_project", nil)}, {}}, texts: []string{"", "Toque em Abrir no Estúdio no cartão."}}
	rig := newOpeningRig(prov, prov, projectCreator(false), read)
	rig.answer(t, rig.send("crie a capa"))

	assertChargedOnceEach(t, prov, 2)
	if offers(prov.inputs[1], "studio_read") {
		t.Fatal("a closed editor offers no studio tools yet")
	}
	if rig.adopter.of("t1") != createdView.ProjectID {
		t.Fatal("even closed, the project she created is the session's project")
	}
	stored := string(rig.ms.last().ToolCalls)
	if !strings.Contains(stored, `"awaits"`) || !strings.Contains(stored, createdView.ProjectID) {
		t.Fatalf("the thread must remember which editor she waits for, got %s", stored)
	}
}

func TestOpeningTheProjectDuringTheAnswerReachesItAtTheNextToolBoundary(t *testing.T) {
	var rig *openingRig
	read := newStudioTool("studio_read", readMeta)
	outcome := make(chan OpenOutcome, 1)
	brief := &hookTool{name: "load_skill", run: func() {
		got, err := rig.svc.ProjectOpened(context.Background(), rig.th.thread, studioCtx(createdView))
		if err != nil {
			t.Errorf("opened: %v", err)
		}
		outcome <- got
	}}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("new_studio_project", nil)}, {call("load_skill", nil)}, {call("studio_read", nil)}, {}}, texts: []string{"", "", "", "Pronto."}}
	rig = newOpeningRig(prov, prov, projectCreator(false), brief, read)
	rig.answer(t, rig.send("crie a capa"))

	if got := <-outcome; got != OpenDelivered {
		t.Fatalf("outcome = %s, the running answer must take the event", got)
	}
	assertChargedOnceEach(t, prov, 4)
	if offers(prov.inputs[1], "studio_read") || !offers(prov.inputs[2], "studio_read") {
		t.Fatal("the studio arrives at the tool boundary after the person opened it")
	}
	if !sees(prov.inputs[2], agentloop.AppNotice(projectOpenedNote(createdView))) {
		t.Fatal("the model must be told by the app that the editor opened")
	}
	for _, in := range prov.inputs {
		for _, m := range in.Messages {
			if strings.Contains(m.Content, agentloop.UserRequest(projectOpenedNote(createdView))) || strings.Contains(m.Content, agentloop.Interjection(projectOpenedNote(createdView))) {
				t.Fatal("the app's notice must never pose as a message from the person")
			}
		}
	}
	if got := roles(rig.ms.created); got != "user,assistant" {
		t.Fatalf("thread = %s, the notice is not a stored message", got)
	}
	if strings.Contains(string(rig.ms.last().ToolCalls), "awaits") {
		t.Fatal("once the editor opened, nothing is waited for")
	}
}

func TestOpeningTheProjectAfterTheAnswerResumesOnceWhenSheWasWaiting(t *testing.T) {
	read := newStudioTool("studio_read", readMeta)
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("studio_read", nil)}, {}}, texts: []string{"", "Comecei pela capa."}}
	rig := newOpeningRig(prov, prov, read)
	rig.awaitingThread()

	got, err := rig.svc.ProjectOpened(context.Background(), rig.th.thread, studioCtx(createdView))
	if err != nil || got != OpenResumed {
		t.Fatalf("outcome = %s %v, she announced work on this editor", got, err)
	}
	rig.follow(t)
	assertChargedOnceEach(t, prov, 2)
	if !sees(prov.inputs[0], agentloop.AppNotice(projectOpenedNote(createdView))) || !offers(prov.inputs[0], "studio_read") {
		t.Fatal("the resumed answer starts from the app's notice, on the studio screen")
	}
	if read.calls != 1 || read.gotCC.View != createdView {
		t.Fatalf("she works on the opened project, got %+v", read.gotCC)
	}
	if got := roles(rig.ms.created); got != "user,assistant,assistant" {
		t.Fatalf("thread = %s, only her reply is stored", got)
	}

	again, err := rig.svc.ProjectOpened(context.Background(), rig.th.thread, studioCtx(createdView))
	if err != nil || again != OpenIgnored || len(prov.inputs) != 2 {
		t.Fatalf("a second open must not resume again, got %s %v with %d calls", again, err, len(prov.inputs))
	}
}

func TestOpeningWithoutAnnouncedWorkCostsNothing(t *testing.T) {
	prov := &scriptAI{}
	rig := newOpeningRig(prov, prov)
	rig.th.thread.ProjectID = createdView.ProjectID
	rig.ms.created = append(rig.ms.created, &aichat.Message{ThreadID: "t1", Role: aichat.RoleAssistant, Content: "Criei o projeto."})

	got, err := rig.svc.ProjectOpened(context.Background(), rig.th.thread, studioCtx(createdView))
	if err != nil || got != OpenIgnored || len(prov.inputs) != 0 || rig.svc.TurnRunning("t1") {
		t.Fatalf("no announced work, no model call: got %s %v calls %d", got, err, len(prov.inputs))
	}
	rig.ms.created = append(rig.ms.created, &aichat.Message{ThreadID: "t1", Role: aichat.RoleUser, Content: "ok"})
	rig.awaitingThread()
	rig.ms.created = append(rig.ms.created, &aichat.Message{ThreadID: "t1", Role: aichat.RoleUser, Content: "deixa que eu mexo"})
	if got, _ := rig.svc.ProjectOpened(context.Background(), rig.th.thread, studioCtx(createdView)); got != OpenIgnored || len(prov.inputs) != 0 {
		t.Fatal("once the person wrote after her, she is no longer waiting")
	}
	rig.awaitingThread()
	rig.ms.last().ProposalID, rig.ms.last().ProposalStatus = "act-9", aichat.ProposalPending
	if got, _ := rig.svc.ProjectOpened(context.Background(), rig.th.thread, studioCtx(createdView)); got != OpenIgnored || len(prov.inputs) != 0 {
		t.Fatal("an approval card that waits keeps the answer waiting for the person, not for the editor")
	}
}

func TestTheWrongProjectIsIgnored(t *testing.T) {
	prov := &scriptAI{}
	rig := newOpeningRig(prov, prov)
	rig.awaitingThread()
	other := createdView
	other.ProjectID = "7b2c3d4e-5f6a-4b7c-8d9e-0f1a2b3c4d5e"

	turn, _ := rig.svc.BeginTurn(context.Background(), "t1", "u1")
	release := holdOpen(turn)
	for name, cc := range map[string]copilot.Context{"another project": studioCtx(other), "outside the studio": ownerCtx} {
		if got, err := rig.svc.ProjectOpened(context.Background(), rig.th.thread, cc); err != nil || got != OpenIgnored {
			t.Errorf("%s: got %s %v", name, got, err)
		}
	}
	if _, offered := rig.svc.turns.TakeOpened("t1"); offered {
		t.Fatal("an ignored open must not reach the running answer")
	}
	stranger := studioCtx(createdView)
	stranger.UserID = "intruder"
	if _, err := rig.svc.ProjectOpened(context.Background(), rig.th.thread, stranger); !errors.Is(err, copilot.ErrTurnForbidden) {
		t.Fatalf("another user cannot touch the running answer, got %v", err)
	}
	close(release)

	rig.th.thread.ProjectID = ""
	if got, _ := rig.svc.ProjectOpened(context.Background(), rig.th.thread, studioCtx(createdView)); got != OpenIgnored || len(prov.inputs) != 0 {
		t.Fatal("a thread not bound to the project ignores it")
	}
}

func TestAnOpenDuringTheFinalCallStartsOneResume(t *testing.T) {
	script := &scriptAI{turns: [][]ai.ToolCall{{call("new_studio_project", nil)}, {}, {}}, texts: []string{"", "Toque em Abrir no Estúdio.", "Abriu, comecei a capa."}}
	var rig *openingRig
	prov := &hookAI{scriptAI: script, before: func(call int) {
		if call == 1 {
			if got, err := rig.svc.ProjectOpened(context.Background(), rig.th.thread, studioCtx(createdView)); err != nil || got != OpenDelivered {
				t.Errorf("opened during the final call: %s %v", got, err)
			}
		}
	}}
	rig = newOpeningRig(prov, script, projectCreator(false), newStudioTool("studio_read", readMeta))
	evs := rig.answer(t, rig.send("crie a capa"))

	assertChargedOnceEach(t, script, 3)
	if count(types(evs), "done") != 2 {
		t.Fatalf("events = %v, want her answer and one resume", types(evs))
	}
	if !sees(script.inputs[2], agentloop.AppNotice(projectOpenedNote(createdView))) || !offers(script.inputs[2], "studio_read") {
		t.Fatal("the resume runs on the studio screen from the app's notice")
	}
}

type parkedAI struct {
	*scriptAI
	mu      sync.Mutex
	parked  bool
	stopped bool
}

func (p *parkedAI) GenerateStream(ctx context.Context, in ai.GenerateInput) (<-chan ai.StreamEvent, error) {
	p.mu.Lock()
	p.parked = true
	p.mu.Unlock()
	<-ctx.Done()
	p.mu.Lock()
	p.stopped = true
	p.mu.Unlock()
	return nil, ctx.Err()
}

func (p *parkedAI) isParked() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.parked
}

func (p *parkedAI) wasStopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopped
}

func TestAResumeNobodyWatchesIsCancelledAndAStoppedOneDoesNotFollowUp(t *testing.T) {
	prov := &parkedAI{scriptAI: &scriptAI{}}
	rig := newOpeningRig(prov, prov.scriptAI)
	limits := testLimits()
	limits.DetachedGrace = 30 * time.Millisecond
	rig.svc.turns = NewTurnRegistry(limits)
	rig.awaitingThread()

	if got, _ := rig.svc.ProjectOpened(context.Background(), rig.th.thread, studioCtx(createdView)); got != OpenResumed {
		t.Fatalf("got %s", got)
	}
	eventually(t, "an unwatched resume ends", func() bool { return !rig.svc.TurnRunning("t1") && prov.wasStopped() })

	stopping := &parkedAI{scriptAI: &scriptAI{}}
	rig = newOpeningRig(stopping, stopping.scriptAI)
	rig.awaitingThread()
	if got, _ := rig.svc.ProjectOpened(context.Background(), rig.th.thread, studioCtx(createdView)); got != OpenResumed {
		t.Fatalf("got %s", got)
	}
	turn, _ := rig.svc.ObserveTurn("t1", "u1")
	_, live, leave := turn.Subscribe()
	defer leave()
	eventually(t, "the resumed answer reaches the model", stopping.isParked)
	if err := rig.svc.StopTurn("t1", "u1"); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, live)
	if !stopping.wasStopped() || rig.svc.TurnRunning("t1") {
		t.Fatal("stop ends the resumed answer")
	}
}

func TestAnUnboundProjectIsNeverOpenedForTheTurn(t *testing.T) {
	read := newStudioTool("studio_read", readMeta)
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("new_studio_project", nil)}, {}}, texts: []string{"", "Não consegui ligar a conversa."}}
	rig := newOpeningRig(prov, prov, projectCreator(true), read)
	rig.adopter.err = errors.New("database down")
	rig.answer(t, rig.send("crie a capa"))

	if offers(prov.inputs[1], "studio_read") || rig.th.thread.ProjectID != "" {
		t.Fatal("without the binding the turn must not move into the studio")
	}
	if !strings.Contains(prov.inputs[1].Messages[len(prov.inputs[1].Messages)-2].Content, "não pôde ser ligada") {
		t.Fatal("the model must hear that the conversation could not be tied to the project")
	}
}
