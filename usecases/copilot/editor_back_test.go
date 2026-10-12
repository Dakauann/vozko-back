package copilot_usecase

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/aichat"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/usecases/agentloop"
)

type studioHook struct {
	name   string
	result copilot.Result
	run    func()
}

func (h *studioHook) Definition() tools.Definition {
	return tools.Definition{Name: h.name, Description: "x"}
}
func (h *studioHook) Meta() copilot.Meta               { return readMeta }
func (h *studioHook) OfferedOn(view copilot.View) bool { return view.OnStudio() }
func (h *studioHook) Execute(context.Context, copilot.Context, map[string]interface{}) copilot.Result {
	if h.run != nil {
		h.run()
	}
	if h.result.Status == "" {
		return copilot.Result{Status: copilot.StatusOK, Data: "lido"}
	}
	return h.result
}

func silentEditor(view copilot.View) copilot.Result {
	return copilot.Result{Status: copilot.StatusError, Message: "o editor não respondeu a tempo", AwaitsScreen: &view}
}

func (r *openingRig) answerOn(t *testing.T, view copilot.View, content string) []copilot.TurnEvent {
	t.Helper()
	turn, err := r.svc.BeginTurn(context.Background(), r.th.thread.ID, "u1")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	_, live, leave := turn.Subscribe()
	defer leave()
	cc := studioCtx(view)
	r.svc.Start(turn, r.th.thread, cc, func(ctx context.Context, emit agentloop.Emit) error {
		return r.svc.Stream(ctx, r.th.thread, copilot.UserMessage{Content: content}, cc, emit)
	})
	return waitClosed(t, live)
}

func noticed(in ai.GenerateInput) bool {
	return sees(in, agentloop.AppNotice(""))
}

func TestASilentEditorStepRemembersWhichEditorItWaitsFor(t *testing.T) {
	step := stepFromResult("studio_edit_image", silentEditor(createdView))
	if step.Ok || step.Awaits == nil || *step.Awaits != createdView {
		t.Fatalf("the failed step must wait for the editor that went silent, got %+v", step)
	}
	answered := stepFromResult("studio_edit_image", copilot.Result{Status: copilot.StatusError, Message: "recusado"})
	if answered.Awaits != nil {
		t.Fatalf("a refusal is an answer and waits for nothing, got %+v", answered)
	}
}

func TestTheEditorAnsweringAgainReachesTheRunningAnswerAtTheNextToolBoundary(t *testing.T) {
	var rig *openingRig
	outcome := make(chan OpenOutcome, 1)
	edit := &studioHook{name: "studio_edit_image", result: silentEditor(createdView)}
	jobs := &studioHook{name: "studio_jobs", run: func() {
		got, err := rig.svc.ProjectOpened(context.Background(), rig.th.thread, studioCtx(createdView))
		if err != nil {
			t.Errorf("visible: %v", err)
		}
		outcome <- got
	}}
	read := &studioHook{name: "studio_read"}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("studio_edit_image", nil)}, {call("studio_jobs", nil)}, {call("studio_read", nil)}, {}}, texts: []string{"", "", "", "Pronto."}}
	rig = newOpeningRig(prov, prov, edit, jobs, read)
	rig.th.thread.ProjectID = createdView.ProjectID
	rig.answerOn(t, createdView, "troque o título")

	if got := <-outcome; got != OpenDelivered {
		t.Fatalf("outcome = %s, the running answer must take the event", got)
	}
	assertChargedOnceEach(t, prov, 4)
	if noticed(prov.inputs[1]) || !sees(prov.inputs[2], agentloop.AppNotice(editorBackNote(createdView))) {
		t.Fatal("the app tells her the editor answers again right after the tool that was running")
	}
	if sees(prov.inputs[2], agentloop.AppNotice(projectOpenedNote(createdView))) {
		t.Fatal("an editor that was already open is not announced as just opened")
	}
	if got := roles(rig.ms.created); got != "user,assistant" {
		t.Fatalf("thread = %s, the notice is never a stored message", got)
	}
	if strings.Contains(string(rig.ms.last().ToolCalls), "awaits") {
		t.Fatal("once the editor answered again, nothing is waited for")
	}
}

func TestAVisibleTabWithoutASilentEditorCostsNothing(t *testing.T) {
	var rig *openingRig
	jobs := &studioHook{name: "studio_jobs", run: func() {
		_, _ = rig.svc.ProjectOpened(context.Background(), rig.th.thread, studioCtx(createdView))
	}}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("studio_jobs", nil)}, {}}, texts: []string{"", "Pronto."}}
	rig = newOpeningRig(prov, prov, jobs)
	rig.th.thread.ProjectID = createdView.ProjectID
	rig.answerOn(t, createdView, "como está?")

	assertChargedOnceEach(t, prov, 2)
	if noticed(prov.inputs[1]) {
		t.Fatal("a tab that becomes visible while the editor always answered adds nothing to the answer")
	}
	prov.inputs = nil
	if got, _ := rig.svc.ProjectOpened(context.Background(), rig.th.thread, studioCtx(createdView)); got != OpenIgnored || len(prov.inputs) != 0 {
		t.Fatalf("after an answer that never lost the editor, a visible tab resumes nothing, got %s", got)
	}
}

func TestTheEditorAnsweringAgainAfterTheAnswerResumesHerOnce(t *testing.T) {
	read := &studioHook{name: "studio_read"}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("studio_read", nil)}, {}}, texts: []string{"", "Voltei ao título."}}
	rig := newOpeningRig(prov, prov, read)
	rig.th.thread.ProjectID = createdView.ProjectID
	steps, _ := json.Marshal([]toolStep{{Name: "studio_edit_image", Summary: "error", Ok: false, Error: "o editor não respondeu a tempo", Awaits: &createdView}})
	rig.ms.created = append(rig.ms.created,
		&aichat.Message{ThreadID: "t1", Role: aichat.RoleUser, Content: "troque o título"},
		&aichat.Message{ThreadID: "t1", Role: aichat.RoleAssistant, Content: "O editor não respondeu; retomo quando ele voltar.", ToolCalls: steps})

	got, err := rig.svc.ProjectOpened(context.Background(), rig.th.thread, studioCtx(createdView))
	if err != nil || got != OpenResumed {
		t.Fatalf("outcome = %s %v, she was waiting for this editor", got, err)
	}
	rig.follow(t)
	assertChargedOnceEach(t, prov, 2)
	if !sees(prov.inputs[0], agentloop.AppNotice(editorBackNote(createdView))) || sees(prov.inputs[0], agentloop.AppNotice(projectOpenedNote(createdView))) {
		t.Fatal("the resumed answer starts from the app saying the editor answers again")
	}
	if got := roles(rig.ms.created); got != "user,assistant,assistant" {
		t.Fatalf("thread = %s, the notice is never a stored message", got)
	}
	if again, _ := rig.svc.ProjectOpened(context.Background(), rig.th.thread, studioCtx(createdView)); again != OpenIgnored || len(prov.inputs) != 2 {
		t.Fatalf("one silence resumes once, got %s with %d calls", again, len(prov.inputs))
	}
}
