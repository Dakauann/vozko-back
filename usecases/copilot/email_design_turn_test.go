package copilot_usecase

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vozko/domain/ai"
	"vozko/domain/aichat"
	"vozko/domain/copilot"
	"vozko/usecases/agentloop"
)

var emailView = copilot.View{Surface: copilot.SurfaceEmailDesign, ProjectID: "8d6f2b1a-3c4e-4f5a-9b7c-1d2e3f4a5b6c"}

type emailTool struct{ fakeTool }

func (emailTool) OfferedOn(view copilot.View) bool { return view.OnEmailDesign() }

func newEmailTool(name string) *emailTool {
	return &emailTool{fakeTool{name: name, meta: readMeta}}
}

func TestEmailToolsExistOnlyInsideTheEmailEditor(t *testing.T) {
	read := newEmailTool("email_read")
	plain := &fakeTool{name: "list_agents", meta: readMeta}
	if got := offeredNames(studioDriver(emailView, read, plain)); got != "email_read" {
		t.Fatalf("the email editor offers only its own tools, got %s", got)
	}
	if got := offeredNames(studioDriver(copilot.View{}, read, plain)); got != "list_agents" {
		t.Fatalf("the panel never offers email editor tools, got %s", got)
	}
	if got := offeredNames(studioDriver(studioView, read, plain)); got != "" {
		t.Fatalf("the studio does not offer email editor tools, got %s", got)
	}
}

func TestTheEmailEditorPromptExplainsTheEditorOnlyThere(t *testing.T) {
	prompt := systemPrompt(emailView, time.Now())
	if !strings.Contains(prompt, "# Editor de e-mail") || !strings.Contains(prompt, emailView.ProjectID) || strings.Contains(prompt, "# Estúdio") {
		t.Fatal("the email surface explains its own editor")
	}
	for _, other := range []copilot.View{{}, studioView} {
		if strings.Contains(systemPrompt(other, time.Now()), "# Editor de e-mail") {
			t.Fatal("the email editor section stays inside the email editor")
		}
	}
	if strings.Contains(prompt, "# Anúncios da Meta") {
		t.Fatal("a focused surface leaves out the guides of other areas")
	}
}

func TestAnEmailTurnTalksToTheOpenDesign(t *testing.T) {
	svc := &Service{screens: newMailbox(), newID: func() string { return "cmd-1" }, turns: NewTurnRegistry(copilot.DefaultTurnLimits())}
	cc := svc.attachScreen(studioCtx(emailView), "t1", func(string, interface{}) {})
	screen, ok := cc.Screen.(*screenSession)
	if !ok || screen.projectID != emailView.ProjectID {
		t.Fatalf("the screen session must target the open design, got %+v", cc.Screen)
	}
}

func emailDesignCreator(opened bool) *fakeTool {
	return &fakeTool{name: "new_email_design", meta: readMeta, result: copilot.Result{
		Status:  copilot.StatusOK,
		Data:    map[string]string{"state": "criado"},
		Opening: &copilot.ProjectOpening{View: emailView, Opened: opened},
	}}
}

func TestAnOpenedEmailEditorContinuesTheSameAnswer(t *testing.T) {
	read := newEmailTool("email_read")
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("new_email_design", nil)}, {call("email_read", nil)}, {}}, texts: []string{"", "", "Comecei pelo topo."}}
	rig := newOpeningRig(prov, prov, emailDesignCreator(true), read)
	rig.answer(t, rig.send("crie um e-mail de Black Friday"))

	if offers(prov.inputs[0], "email_read") || !offers(prov.inputs[1], "email_read") {
		t.Fatal("the email editor tools arrive right after the editor opened")
	}
	if !strings.Contains(prov.inputs[1].SystemPrompt, "# Editor de e-mail") {
		t.Fatal("from then on the answer runs on the email editor of the new design")
	}
	if rig.adopter.of("t1") != emailView.ProjectID || read.gotCC.View != emailView {
		t.Fatal("the thread becomes the session of the design she created")
	}
}

func TestOpeningTheNewDesignLaterResumesTheAnswer(t *testing.T) {
	read := newEmailTool("email_read")
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("email_read", nil)}, {}}, texts: []string{"", "Comecei pelo topo."}}
	rig := newOpeningRig(prov, prov, read)
	steps, _ := json.Marshal([]toolStep{{Name: "new_email_design", Summary: "ok", Ok: true, Awaits: &emailView}})
	rig.th.thread.ProjectID = emailView.ProjectID
	rig.ms.created = append(rig.ms.created,
		&aichat.Message{ThreadID: "t1", Role: aichat.RoleUser, Content: "crie o e-mail"},
		&aichat.Message{ThreadID: "t1", Role: aichat.RoleAssistant, Content: "Toque em Abrir no editor.", ToolCalls: steps})

	got, err := rig.svc.ProjectOpened(context.Background(), rig.th.thread, studioCtx(emailView))
	if err != nil || got != OpenResumed {
		t.Fatalf("outcome = %s %v", got, err)
	}
	rig.follow(t)
	note := projectOpenedNote(emailView)
	if !sees(prov.inputs[0], agentloop.AppNotice(note)) || !strings.Contains(note, "email_read") || strings.Contains(note, "studio_read") {
		t.Fatalf("the resume note points at the email editor tools, got %q", note)
	}
	if read.calls != 1 || read.gotCC.View != emailView {
		t.Fatalf("she works on the opened design, got %+v", read.gotCC)
	}
	if back := editorBackNote(emailView); !strings.Contains(back, "email_read") || strings.Contains(back, "studio_read") {
		t.Fatalf("the editor back note points at the email tools, got %q", back)
	}
}

type watchedEmailTool struct {
	emailTool
	snapshot string
	note     string
	replies  []string
}

func (w *watchedEmailTool) ViewSnapshot(context.Context, copilot.Context) string { return w.snapshot }

func (w *watchedEmailTool) ClosingNote(_ context.Context, _ copilot.Context, reply string) string {
	w.replies = append(w.replies, reply)
	return w.note
}

func emailTurn(t *testing.T, watch *watchedEmailTool, prov *scriptAI) {
	t.Helper()
	th := &fakeThreads{thread: &aichat.Thread{ID: "th1", WorkspaceID: "ws1", UserID: "u1", ProjectID: emailView.ProjectID}}
	svc := newService(prov, th, &fakeMessages{}, watch)
	svc.SetScreenMailbox(newMailbox())
	cc := ownerCtx
	cc.View = emailView
	if err := svc.Stream(context.Background(), th.thread, copilot.UserMessage{Content: "e agora?"}, cc, (&capture{}).emit); err != nil {
		t.Fatal(err)
	}
}

func TestAnEmailTurnStartsWithTheOpenGapsInView(t *testing.T) {
	watch := &watchedEmailTool{emailTool: *newEmailTool("email_read"), snapshot: "\n\n# Design aberto\n- cta (button \"Agendar\"): href_missing"}
	prov := &scriptAI{turns: [][]ai.ToolCall{{}}, texts: []string{"Falta o link do botão."}}
	emailTurn(t, watch, prov)
	msgs := prov.inputs[0].Messages
	if !strings.Contains(msgs[len(msgs)-1].Content, "href_missing") {
		t.Fatal("the editor snapshot reaches her with the open gaps")
	}
}

func TestOpenGapsLeftOutOfTheReplyAreNotedOnce(t *testing.T) {
	watch := &watchedEmailTool{emailTool: *newEmailTool("email_read"), note: "o botão cta ainda não tem link"}
	prov := &scriptAI{turns: [][]ai.ToolCall{{}, {}, {}}, texts: []string{"Pronto!", "Falta o link do botão.", "nunca"}}
	emailTurn(t, watch, prov)
	if len(prov.inputs) != 2 || len(watch.replies) != 1 || !sees(prov.inputs[1], agentloop.AppNotice("o botão cta ainda não tem link")) {
		t.Fatalf("one note and one more answer, got %d calls and %d checks", len(prov.inputs), len(watch.replies))
	}
}

func TestNoOpenGapsEndTheAnswerAtOnce(t *testing.T) {
	watch := &watchedEmailTool{emailTool: *newEmailTool("email_read")}
	prov := &scriptAI{turns: [][]ai.ToolCall{{}, {}}, texts: []string{"Pronto!", "nunca"}}
	emailTurn(t, watch, prov)
	if len(prov.inputs) != 1 || len(watch.replies) != 1 {
		t.Fatalf("no gaps, no note: got %d calls", len(prov.inputs))
	}
}

func TestOtherSurfacesAreNeverWatched(t *testing.T) {
	watch := &watchedEmailTool{emailTool: *newEmailTool("email_read"), snapshot: "gaps", note: "gaps"}
	d := studioDriver(copilot.View{}, watch)
	d.observeView(context.Background())
	if d.ClosingNote(context.Background(), "Pronto!") != "" || strings.Contains(d.Reground(1, 1, 0), "gaps") || len(watch.replies) != 0 {
		t.Fatal("only the surface that offers the watching tool is watched")
	}
}
