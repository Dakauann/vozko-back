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
	"vozko/domain/workspace"
	"vozko/usecases/agentloop"
)

var studioView = copilot.View{Surface: copilot.SurfaceStudio, ProjectID: "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f", ProjectKind: copilot.StudioVideo}

type studioTool struct{ fakeTool }

func (studioTool) OfferedOn(view copilot.View) bool { return view.OnStudio() }

func newStudioTool(name string, meta copilot.Meta) *studioTool {
	return &studioTool{fakeTool{name: name, meta: meta}}
}

func studioDriver(view copilot.View, ts ...copilot.Tool) *Driver {
	cc := ownerCtx
	cc.View = view
	return NewDriver(cc, "m", NewRegistry(ts...), &fakeAccess{}, openFunds{}, func() string { return "act-1" })
}

func offeredNames(d *Driver) string {
	names := []string{}
	for _, def := range d.Tools() {
		names = append(names, def.Name)
	}
	return strings.Join(names, ",")
}

func TestStudioToolsExistOnlyInsideTheStudio(t *testing.T) {
	edit := newStudioTool("studio_edit", readMeta)
	plain := &fakeTool{name: "list_agents", meta: readMeta}
	if got := offeredNames(studioDriver(copilot.View{}, edit, plain)); got != "list_agents" {
		t.Fatalf("outside the studio the offer must be unchanged, got %q", got)
	}
	if got := offeredNames(studioDriver(studioView, edit, plain)); got != "studio_edit" {
		t.Fatalf("inside the studio the studio tool is offered, got %q", got)
	}
	step := studioDriver(copilot.View{Surface: copilot.SurfaceAttendance}, edit).Dispatch(context.Background(), call("studio_edit", nil), (&capture{}).emit)
	if edit.calls != 0 || !strings.Contains(step.Result, "INDISPONÍVEL") {
		t.Fatalf("a studio tool called elsewhere must be refused, got calls=%d step=%+v", edit.calls, step)
	}
	res := studioDriver(copilot.View{}, edit).ExecuteApproved(context.Background(), copilot.PendingAction{ToolName: "studio_edit"}, copilot.Approval{}, (&capture{}).emit)
	if res.Status != copilot.StatusDenied || edit.calls != 0 {
		t.Fatalf("an approval outside the studio must not run a studio tool, got %+v", res)
	}
}

func TestToolImagesAndEndOfTurnReachTheLoop(t *testing.T) {
	look := newStudioTool("studio_look", readMeta)
	look.result = copilot.Result{Status: copilot.StatusOK, Data: "6 quadros", Images: []string{"data:image/jpeg;base64,AAAA"}, EndTurn: true}
	step := studioDriver(studioView, look).Dispatch(context.Background(), call("studio_look", nil), (&capture{}).emit)
	if len(step.Images) != 1 || !step.EndTurn {
		t.Fatalf("images and end of turn must pass through, got %+v", step)
	}
	look.result = copilot.Result{Status: copilot.StatusError, Message: "falhou", Images: []string{"data:image/jpeg;base64,AAAA"}}
	if step := studioDriver(studioView, look).Dispatch(context.Background(), call("studio_look", nil), (&capture{}).emit); len(step.Images) != 0 {
		t.Fatal("a failed tool must not send images")
	}
}

type mailbox struct {
	mu      sync.Mutex
	pending map[string]chan copilot.ScreenReply
	opened  []string
}

func newMailbox() *mailbox { return &mailbox{pending: map[string]chan copilot.ScreenReply{}} }

func (m *mailbox) Expect(key string) (func(context.Context) (copilot.ScreenReply, error), func()) {
	ch := make(chan copilot.ScreenReply, 1)
	m.mu.Lock()
	m.pending[key] = ch
	m.opened = append(m.opened, key)
	m.mu.Unlock()
	wait := func(ctx context.Context) (copilot.ScreenReply, error) {
		select {
		case r := <-ch:
			return r, nil
		case <-ctx.Done():
			return copilot.ScreenReply{}, ctx.Err()
		}
	}
	return wait, func() {
		m.mu.Lock()
		delete(m.pending, key)
		m.mu.Unlock()
	}
}

func (m *mailbox) Deliver(key string, reply copilot.ScreenReply) error {
	m.mu.Lock()
	ch, ok := m.pending[key]
	m.mu.Unlock()
	if ok {
		ch <- reply
	}
	return nil
}

type screenEmit struct {
	box      *mailbox
	threadID string
	reply    copilot.ScreenReply
	commands []copilot.ScreenCommand
}

func (s *screenEmit) emit(eventType string, payload interface{}) {
	if eventType != EventScreenCommand {
		return
	}
	cmd := payload.(copilot.ScreenCommand)
	s.commands = append(s.commands, cmd)
	go func() { _ = s.box.Deliver(copilot.ScreenKey(s.threadID, cmd.ID), s.reply) }()
}

func TestScreenSessionSendsTheCommandAndReturnsTheEditorReply(t *testing.T) {
	box := newMailbox()
	out := &screenEmit{box: box, threadID: "th1", reply: copilot.ScreenReply{OK: true, Data: []byte(`{"tracks":[]}`)}}
	screen := &screenSession{threadID: "th1", projectID: studioView.ProjectID, mailbox: box, emit: out.emit, newID: func() string { return "cmd-1" }}
	reply, err := screen.Run(context.Background(), copilot.ScreenCommand{Name: copilot.ScreenRead})
	if err != nil || !reply.OK || string(reply.Data) != `{"tracks":[]}` {
		t.Fatalf("Run() = %+v, %v", reply, err)
	}
	if len(out.commands) != 1 || out.commands[0].ID != "cmd-1" || out.commands[0].ProjectID != studioView.ProjectID {
		t.Fatalf("the command must name itself and the project, got %+v", out.commands)
	}
	if box.opened[0] != copilot.ScreenKey("th1", "cmd-1") {
		t.Fatalf("the reply must be awaited on the thread's key, got %v", box.opened)
	}
}

func TestScreenSessionFailsClosedWhenTheEditorIsSilent(t *testing.T) {
	screen := &screenSession{threadID: "th1", projectID: studioView.ProjectID, mailbox: newMailbox(), emit: func(string, interface{}) {}, newID: func() string { return "cmd-1" }}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := screen.Run(ctx, copilot.ScreenCommand{Name: copilot.ScreenRead}); !errors.Is(err, copilot.ErrScreenUnavailable) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a silent editor must fail closed, got %v", err)
	}
	if _, err := screen.Run(context.Background(), copilot.ScreenCommand{Name: "click"}); err == nil {
		t.Fatal("an unknown command must not be sent")
	}
}

func TestAStudioTurnCarriesTheScreenAndTheModelsSight(t *testing.T) {
	th := &fakeThreads{thread: &aichat.Thread{ID: "th1", WorkspaceID: "ws1", UserID: "u1", Model: "vision-model"}}
	look := newStudioTool("studio_look", copilot.Meta{Resource: workspace.ResourceMedia, Action: workspace.ActionRead})
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("studio_look", nil)}, {}}, texts: []string{"", "vi"}, models: []ai.ModelInfo{{ID: "vision-model", SeesImages: true}}}
	svc := newService(prov, th, &fakeMessages{}, look)
	svc.SetScreenMailbox(newMailbox())
	cc := ownerCtx
	cc.View = studioView
	if err := svc.Stream(context.Background(), th.thread, copilot.UserMessage{Content: "veja"}, cc, (&capture{}).emit); err != nil {
		t.Fatal(err)
	}
	if look.gotCC.Screen == nil || !look.gotCC.SeesImages {
		t.Fatalf("a studio tool must receive the screen and the model's sight, got %+v", look.gotCC)
	}

	plain := &fakeTool{name: "list_agents", meta: readMeta}
	prov = &scriptAI{turns: [][]ai.ToolCall{{call("list_agents", nil)}, {}}, texts: []string{"", "ok"}}
	svc = newService(prov, th, &fakeMessages{}, plain)
	svc.SetScreenMailbox(newMailbox())
	if err := svc.Stream(context.Background(), th.thread, copilot.UserMessage{Content: "liste"}, ownerCtx, (&capture{}).emit); err != nil {
		t.Fatal(err)
	}
	if plain.gotCC.Screen != nil {
		t.Fatal("outside the studio no screen is attached")
	}
}

func TestScreenRepliesAreDeliveredOnlyToTheirThread(t *testing.T) {
	box := newMailbox()
	wait, cancel := box.Expect(copilot.ScreenKey("th1", "cmd-1"))
	defer cancel()
	svc := newService(&scriptAI{}, &fakeThreads{}, &fakeMessages{})
	if err := svc.DeliverScreenReply("th1", "cmd-1", copilot.ScreenReply{OK: true}); !errors.Is(err, copilot.ErrNoScreen) {
		t.Fatalf("without a mailbox nothing is delivered, got %v", err)
	}
	svc.SetScreenMailbox(box)
	if err := svc.DeliverScreenReply("th1", "cmd-1", copilot.ScreenReply{OK: true}); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if reply, err := wait(ctx); err != nil || !reply.OK {
		t.Fatalf("the waiting command must receive its reply, got %+v %v", reply, err)
	}
}

func TestTheStudioPromptExplainsTheEditorOnlyInTheStudio(t *testing.T) {
	if !strings.Contains(systemPrompt(studioView, time.Now()), "# Estúdio") {
		t.Fatal("the studio surface must explain the editor")
	}
	if strings.Contains(systemPrompt(copilot.View{}, time.Now()), "# Estúdio") {
		t.Fatal("the studio section must not leak outside the studio")
	}
}

var _ agentloop.Driver = (*Driver)(nil)

func TestAStudioOutlineReachesTheModelButIsNotKeptInHistory(t *testing.T) {
	th := &fakeThreads{thread: &aichat.Thread{ID: "th1", WorkspaceID: "ws1", UserID: "u1"}}
	read := newStudioTool("studio_read", readMeta)
	read.result = copilot.Result{Status: copilot.StatusOK, Data: map[string]string{"outline": "faixa 1: 3 clipes"}}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("studio_read", nil)}, {}}, texts: []string{"", "pronto"}}
	ms := &fakeMessages{}
	svc := newService(prov, th, ms, read)
	svc.SetScreenMailbox(newMailbox())
	cc := ownerCtx
	cc.View = studioView
	if err := svc.Stream(context.Background(), th.thread, copilot.UserMessage{Content: "leia"}, cc, (&capture{}).emit); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prov.inputs[1].Messages[len(prov.inputs[1].Messages)-2].Content, "faixa 1") {
		t.Fatal("the model must see the outline in this answer")
	}
	stored := string(ms.created[len(ms.created)-1].ToolCalls)
	if !strings.Contains(stored, "studio_read") || strings.Contains(stored, "faixa 1") {
		t.Fatalf("the outline must not be stored in history, got %s", stored)
	}
}

func TestTheStudioPromptLeavesOutTheGuidesOfOtherAreas(t *testing.T) {
	studio := systemPrompt(studioView, time.Now())
	general := systemPrompt(copilot.View{}, time.Now())
	if strings.Contains(studio, adsPrompt) || strings.Contains(studio, areasPrompt) {
		t.Fatal("the studio prompt must not carry the ads and areas guides")
	}
	if !strings.Contains(general, adsPrompt) {
		t.Fatal("everywhere else the prompt keeps them")
	}
	if len(studio) >= len(general)/2 {
		t.Fatalf("the studio prompt should be far shorter: %d vs %d", len(studio), len(general))
	}
}

func TestStudioAnswersHaveRoomForEditBatches(t *testing.T) {
	cc := ownerCtx
	cc.View = studioView
	studio := DefaultConfig(cc, ai.ModelInfo{}, 0)
	if studio.MaxTokensPerGen != studioMaxTokensPerGen || studio.ReasoningMaxTokens != studioReasoningMaxTokens {
		t.Fatalf("studio budget = %d output, %d reasoning", studio.MaxTokensPerGen, studio.ReasoningMaxTokens)
	}
	if studio.MaxTokensPerGen-studio.ReasoningMaxTokens < 16000 {
		t.Fatal("after reasoning, a studio answer must still fit a full edit batch")
	}
	if got := DefaultConfig(ownerCtx, ai.ModelInfo{}, 0).MaxTokensPerGen; got != answerMaxTokensPerGen {
		t.Fatalf("general output budget = %d", got)
	}
}

func TestACutOffAnswerIsExplainedToThePerson(t *testing.T) {
	if !strings.Contains(haltMessage(agentloop.ErrOutputTruncated), "cortada") {
		t.Fatal("a truncated answer must reach the person as a message, never as silence")
	}
}

type gradedTool struct {
	studioTool
	approveIn map[copilot.Mode]bool
	choices   []copilot.ChoiceField
}

func (g *gradedTool) NeedsApproval(mode copilot.Mode) bool { return g.approveIn[mode] }

func (g *gradedTool) Choices(context.Context, copilot.Context, map[string]interface{}) ([]copilot.ChoiceField, error) {
	return g.choices, nil
}

func modeDriver(mode copilot.Mode, ts ...copilot.Tool) *Driver {
	cc := ownerCtx
	cc.View = studioView
	cc.Mode = mode
	return NewDriver(cc, "m", NewRegistry(ts...), &fakeAccess{}, openFunds{}, func() string { return "act-1" })
}

func paidGeneration() *gradedTool {
	return &gradedTool{
		studioTool: studioTool{fakeTool{name: "studio_generate_music", meta: writeMeta}},
		approveIn:  map[copilot.Mode]bool{copilot.ModeAsk: true, copilot.ModeEdit: true},
		choices:    []copilot.ChoiceField{{Key: "music_model", Kind: copilot.ChoiceMusicModel, Default: "google/lyria"}},
	}
}

func TestFullAccessRunsAPaidGenerationWithTheRecommendedModel(t *testing.T) {
	gen := paidGeneration()
	step := modeDriver(copilot.ModeFull, gen).Dispatch(context.Background(), call("studio_generate_music", map[string]interface{}{"prompt": "lofi"}), (&capture{}).emit)
	if step.Pause != nil || gen.calls != 1 || gen.gotArgs["music_model"] != "google/lyria" {
		t.Fatalf("full access must run it with the default model, got step %+v args %v", step, gen.gotArgs)
	}
}

func TestOtherModesStillAskForPaidGenerations(t *testing.T) {
	for _, mode := range []copilot.Mode{copilot.ModeAsk, copilot.ModeEdit} {
		gen := paidGeneration()
		step := modeDriver(mode, gen).Dispatch(context.Background(), call("studio_generate_music", nil), (&capture{}).emit)
		if step.Pause == nil || gen.calls != 0 {
			t.Fatalf("%s must propose, got %+v", mode, step)
		}
	}
}

func TestFullAccessFallsBackToTheCardWhenNoModelIsRecommended(t *testing.T) {
	gen := paidGeneration()
	gen.choices = []copilot.ChoiceField{{Key: "image_model", Kind: copilot.ChoiceImageModel}}
	step := modeDriver(copilot.ModeFull, gen).Dispatch(context.Background(), call("studio_generate_music", nil), (&capture{}).emit)
	if step.Pause == nil || gen.calls != 0 {
		t.Fatalf("without a default the user must choose, got %+v", step)
	}
}

func TestFullAccessStillChecksTheChangeBeforeRunning(t *testing.T) {
	gen := paidGeneration()
	gen.invalid = errors.New("prompt vazio")
	step := modeDriver(copilot.ModeFull, gen).Dispatch(context.Background(), call("studio_generate_music", nil), (&capture{}).emit)
	if gen.calls != 0 || !strings.Contains(step.Result, "prompt vazio") {
		t.Fatalf("a refused preflight must not run, got %+v", step)
	}
}

func TestAskModeProposesStudioEdits(t *testing.T) {
	edit := &gradedTool{studioTool: studioTool{fakeTool{name: "studio_edit_video", meta: readMeta}}, approveIn: map[copilot.Mode]bool{copilot.ModeAsk: true}}
	if step := modeDriver(copilot.ModeAsk, edit).Dispatch(context.Background(), call("studio_edit_video", nil), (&capture{}).emit); step.Pause == nil || edit.calls != 0 {
		t.Fatalf("ask mode must propose the edit, got %+v", step)
	}
	if step := modeDriver(copilot.ModeEdit, edit).Dispatch(context.Background(), call("studio_edit_video", nil), (&capture{}).emit); step.Pause != nil || edit.calls != 1 {
		t.Fatalf("edit mode must apply it, got %+v", step)
	}
}

func TestModesNeverLoosenChangesOutsideTheStudio(t *testing.T) {
	wt := &fakeTool{name: "send_message", meta: writeMeta}
	cc := ownerCtx
	cc.Mode = copilot.ModeFull
	drv := NewDriver(cc, "m", NewRegistry(wt), &fakeAccess{}, openFunds{}, func() string { return "act-1" })
	if step := drv.Dispatch(context.Background(), call("send_message", nil), (&capture{}).emit); step.Pause == nil || wt.calls != 0 {
		t.Fatalf("full access must not skip the card for ungraded changes, got %+v", step)
	}
}

type racingTabs struct {
	box      *mailbox
	threadID string
}

func (r *racingTabs) emit(eventType string, payload interface{}) {
	if eventType != EventScreenCommand {
		return
	}
	key := copilot.ScreenKey(r.threadID, payload.(copilot.ScreenCommand).ID)
	go func() {
		_ = r.box.Deliver(key, copilot.ScreenReply{Error: &copilot.ScreenError{Code: copilot.ScreenNoEditor, Message: "o projeto não está aberto nesta aba"}})
		time.Sleep(20 * time.Millisecond)
		_ = r.box.Deliver(key, copilot.ScreenReply{OK: true, Data: []byte(`{"image":"ok"}`)})
	}()
}

func TestATabWithoutTheEditorCannotAnswerForTheEditor(t *testing.T) {
	box := newMailbox()
	tabs := &racingTabs{box: box, threadID: "th1"}
	screen := &screenSession{threadID: "th1", projectID: studioView.ProjectID, mailbox: box, emit: tabs.emit, newID: func() string { return "cmd-1" }}
	reply, err := screen.Run(context.Background(), copilot.ScreenCommand{Name: copilot.ScreenLook})
	if err != nil || !reply.OK || string(reply.Data) != `{"image":"ok"}` {
		t.Fatalf("the editor's answer must win over a faster decline, got %+v %v", reply, err)
	}
}

func TestWhenEveryTabDeclinesTheTurnHearsTheProjectIsNotOpen(t *testing.T) {
	box := newMailbox()
	out := &screenEmit{box: box, threadID: "th1", reply: copilot.ScreenReply{Error: &copilot.ScreenError{Code: copilot.ScreenNoEditor, Message: "o projeto não está aberto nesta aba"}}}
	screen := &screenSession{threadID: "th1", projectID: studioView.ProjectID, mailbox: box, emit: out.emit, newID: func() string { return "cmd-1" }}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	reply, err := screen.Run(ctx, copilot.ScreenCommand{Name: copilot.ScreenRead})
	if err != nil || !reply.Declined() {
		t.Fatalf("after waiting, a decline must be reported as the project not being open, got %+v %v", reply, err)
	}
}
