package copilot_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"vozko/domain/copilot"
	"vozko/usecases/agentloop"
)

func testLimits() copilot.TurnLimits {
	l := copilot.DefaultTurnLimits()
	l.DetachedGrace = time.Minute
	l.Retention = time.Minute
	return l
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func receive(t *testing.T, live <-chan copilot.TurnEvent) (copilot.TurnEvent, bool) {
	t.Helper()
	select {
	case ev, ok := <-live:
		return ev, ok
	case <-time.After(2 * time.Second):
		t.Fatal("no event arrived")
		return copilot.TurnEvent{}, false
	}
}

func waitClosed(t *testing.T, live <-chan copilot.TurnEvent) []copilot.TurnEvent {
	t.Helper()
	var got []copilot.TurnEvent
	for {
		ev, ok := receive(t, live)
		if !ok {
			return got
		}
		got = append(got, ev)
	}
}

func types(events []copilot.TurnEvent) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.Type
	}
	return out
}

func holdOpen(turn *Turn) chan struct{} {
	release := make(chan struct{})
	turn.Run(func(ctx context.Context, emit agentloop.Emit) error {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	})
	return release
}

func TestASecondAnswerOnTheSameThreadIsRefused(t *testing.T) {
	reg := NewTurnRegistry(testLimits())
	first, err := reg.Begin(context.Background(), "th1", "u1")
	if err != nil {
		t.Fatal(err)
	}
	release := holdOpen(first)
	if _, err := reg.Begin(context.Background(), "th1", "u1"); !errors.Is(err, copilot.ErrTurnRunning) {
		t.Fatalf("a running thread must refuse a second answer, got %v", err)
	}
	if _, err := reg.Begin(context.Background(), "th2", "u1"); err != nil {
		t.Fatalf("another thread is independent, got %v", err)
	}
	if !reg.Running("th1") {
		t.Fatal("the thread must report its running answer")
	}
	close(release)
	eventually(t, "the answer to finish", func() bool { return !reg.Running("th1") })
	if _, err := reg.Begin(context.Background(), "th1", "u1"); err != nil {
		t.Fatalf("a finished answer must not block the next one, got %v", err)
	}
}

func TestALateSubscriberReplaysTheTurnThenFollowsIt(t *testing.T) {
	reg := NewTurnRegistry(testLimits())
	turn, _ := reg.Begin(context.Background(), "th1", "u1")
	release := make(chan struct{})
	turn.Run(func(ctx context.Context, emit agentloop.Emit) error {
		emit("assistant_delta", map[string]string{"text": "Ol"})
		emit("assistant_delta", map[string]string{"text": "á"})
		emit("tool", map[string]interface{}{"name": "studio_read", "ok": true})
		<-release
		emit("done", map[string]interface{}{"content": "Olá"})
		return nil
	})
	eventually(t, "the first events", func() bool {
		replay, _, leave := turn.Subscribe()
		defer leave()
		return len(replay) == 2
	})
	replay, live, leave := turn.Subscribe()
	defer leave()
	if got := types(replay); got[0] != "assistant_delta" || got[1] != "tool" || string(replay[0].Payload) != `{"text":"Olá"}` {
		t.Fatalf("the replay must hold the turn so far, got %v %s", got, replay[0].Payload)
	}
	close(release)
	rest := waitClosed(t, live)
	if len(rest) != 1 || rest[0].Type != "done" {
		t.Fatalf("the subscriber must follow the turn to its end, got %v", types(rest))
	}
}

func TestAPendingScreenCommandReplaysUntilSettled(t *testing.T) {
	reg := NewTurnRegistry(testLimits())
	turn, _ := reg.Begin(context.Background(), "th1", "u1")
	_, live, leave := turn.Subscribe()
	defer leave()
	turn.Emit(EventScreenCommand, copilot.ScreenCommand{ID: "cmd-1", Name: copilot.ScreenEdit, ProjectID: "p1"})
	if ev, _ := receive(t, live); ev.Type != EventScreenCommand {
		t.Fatalf("a watching editor must receive the command live, got %s", ev.Type)
	}
	replay, _, leaveLate := turn.Subscribe()
	leaveLate()
	if len(replay) != 1 || replay[0].Type != EventScreenCommand {
		t.Fatalf("an unanswered command must reach a late subscriber, got %v", types(replay))
	}
	var cmd copilot.ScreenCommand
	if err := json.Unmarshal(replay[0].Payload, &cmd); err != nil || cmd.ID != "cmd-1" {
		t.Fatalf("the replayed command must keep its id, got %s", replay[0].Payload)
	}
	reg.Settle("th1", "cmd-1")
	replay, _, leaveAfter := turn.Subscribe()
	leaveAfter()
	if len(replay) != 0 {
		t.Fatalf("a settled command must never run twice, got %v", types(replay))
	}
}

func TestStopEndsTheTurnForItsOwnerOnly(t *testing.T) {
	reg := NewTurnRegistry(testLimits())
	turn, _ := reg.Begin(context.Background(), "th1", "u1")
	holdOpen(turn)
	if err := reg.Stop("th1", "u2"); !errors.Is(err, copilot.ErrTurnForbidden) {
		t.Fatalf("someone else must not stop the answer, got %v", err)
	}
	if turn.Context().Err() != nil {
		t.Fatal("a refused stop must leave the answer running")
	}
	if err := reg.Stop("th1", "u1"); err != nil {
		t.Fatal(err)
	}
	if turn.Context().Err() == nil {
		t.Fatal("the owner's stop must end the answer")
	}
	if err := reg.Stop("missing", "u1"); !errors.Is(err, copilot.ErrTurnNotRunning) {
		t.Fatalf("stopping nothing must say so, got %v", err)
	}
	eventually(t, "the stopped answer to finish", func() bool { return !reg.Running("th1") })
	if err := reg.Stop("th1", "u1"); !errors.Is(err, copilot.ErrTurnNotRunning) {
		t.Fatalf("a finished answer is not running, got %v", err)
	}
}

func TestAClientDisconnectDoesNotEndTheTurn(t *testing.T) {
	reg := NewTurnRegistry(testLimits())
	request, disconnect := context.WithCancel(context.WithValue(context.Background(), ctxKey("who"), "u1"))
	turn, _ := reg.Begin(request, "th1", "u1")
	disconnect()
	if turn.Context().Err() != nil {
		t.Fatal("the answer must outlive the request that started it")
	}
	if turn.Context().Value(ctxKey("who")) != "u1" {
		t.Fatal("the answer must keep the request values")
	}
}

type ctxKey string

func TestATurnNobodyWatchesEndsAfterTheGrace(t *testing.T) {
	limits := testLimits()
	limits.DetachedGrace = 30 * time.Millisecond
	reg := NewTurnRegistry(limits)

	watched, _ := reg.Begin(context.Background(), "th1", "u1")
	_, _, leave := watched.Subscribe()
	time.Sleep(4 * limits.DetachedGrace)
	if watched.Context().Err() != nil {
		t.Fatal("an answer someone watches must keep running")
	}
	leave()
	eventually(t, "the abandoned answer to end", func() bool { return watched.Context().Err() != nil })

	returned, _ := reg.Begin(context.Background(), "th2", "u1")
	_, _, leaveFirst := returned.Subscribe()
	leaveFirst()
	_, _, leaveSecond := returned.Subscribe()
	defer leaveSecond()
	time.Sleep(4 * limits.DetachedGrace)
	if returned.Context().Err() != nil {
		t.Fatal("a client that comes back within the grace keeps the answer")
	}
}

func TestAFinishedTurnStaysObservableForItsRetention(t *testing.T) {
	limits := testLimits()
	limits.Retention = 40 * time.Millisecond
	reg := NewTurnRegistry(limits)
	turn, _ := reg.Begin(context.Background(), "th1", "u1")
	turn.Run(func(ctx context.Context, emit agentloop.Emit) error {
		emit("done", map[string]interface{}{"content": "ok"})
		return nil
	})
	eventually(t, "the answer to finish", func() bool { return !reg.Running("th1") })
	kept, err := reg.Observe("th1", "u1")
	if err != nil {
		t.Fatalf("a just finished answer must still replay, got %v", err)
	}
	replay, live, leave := kept.Subscribe()
	defer leave()
	if got := types(replay); len(got) != 1 || got[0] != "done" {
		t.Fatalf("the replay must end with the finish, got %v", got)
	}
	if _, open := receive(t, live); open {
		t.Fatal("a finished answer has nothing live to follow")
	}
	eventually(t, "the retention to expire", func() bool {
		_, err := reg.Observe("th1", "u1")
		return errors.Is(err, copilot.ErrTurnNotRunning)
	})
}

func TestObservingIsOwnerScoped(t *testing.T) {
	reg := NewTurnRegistry(testLimits())
	turn, _ := reg.Begin(context.Background(), "th1", "u1")
	holdOpen(turn)
	if _, err := reg.Observe("th1", "u2"); !errors.Is(err, copilot.ErrTurnForbidden) {
		t.Fatalf("someone else must not watch the answer, got %v", err)
	}
	if _, err := reg.Observe("missing", "u1"); !errors.Is(err, copilot.ErrTurnNotRunning) {
		t.Fatalf("watching nothing must say so, got %v", err)
	}
	if got, err := reg.Observe("th1", "u1"); err != nil || got != turn {
		t.Fatalf("the owner watches the running answer, got %v %v", got, err)
	}
}

func TestARunThatFailsOrPanicsEndsWithAnError(t *testing.T) {
	for name, run := range map[string]func(context.Context, agentloop.Emit) error{
		"error": func(context.Context, agentloop.Emit) error { return errors.New("saldo insuficiente") },
		"panic": func(context.Context, agentloop.Emit) error { panic("boom") },
	} {
		reg := NewTurnRegistry(testLimits())
		turn, _ := reg.Begin(context.Background(), "th1", "u1")
		_, live, leave := turn.Subscribe()
		turn.Run(run)
		got := waitClosed(t, live)
		leave()
		if len(got) != 1 || got[0].Type != "error" {
			t.Fatalf("%s: the subscriber must learn the answer failed, got %v", name, types(got))
		}
		if reg.Running("th1") {
			t.Fatalf("%s: a failed answer is over", name)
		}
	}
}

func TestASubscriberThatFallsBehindIsDroppedWithoutBlockingTheTurn(t *testing.T) {
	limits := testLimits()
	limits.SubscriberBuffer = 1
	reg := NewTurnRegistry(limits)
	turn, _ := reg.Begin(context.Background(), "th1", "u1")
	_, live, leave := turn.Subscribe()
	defer leave()
	finished := make(chan struct{})
	go func() {
		for i := 0; i < 5; i++ {
			turn.Emit("tool", map[string]int{"n": i})
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("a slow subscriber must not block the answer")
	}
	if got := waitClosed(t, live); len(got) != 1 {
		t.Fatalf("the slow subscriber keeps what fit and is closed, got %d events", len(got))
	}
}

func TestTheServiceGuardsAndStopsTurns(t *testing.T) {
	svc := newService(&scriptAI{}, &fakeThreads{}, &fakeMessages{})
	turn, err := svc.BeginTurn(context.Background(), "th1", "u1")
	if err != nil {
		t.Fatal(err)
	}
	holdOpen(turn)
	if _, err := svc.BeginTurn(context.Background(), "th1", "u1"); !errors.Is(err, copilot.ErrTurnRunning) {
		t.Fatalf("the service must refuse a second answer, got %v", err)
	}
	if !svc.TurnRunning("th1") {
		t.Fatal("the service must report the running answer")
	}
	if _, err := svc.ObserveTurn("th1", "u1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.StopTurn("th1", "u1"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the stopped answer to finish", func() bool { return !svc.TurnRunning("th1") })
}

func TestAScreenCommandIsSettledWhateverTheEditorDoes(t *testing.T) {
	box := newMailbox()
	var settled []string
	out := &screenEmit{box: box, threadID: "th1", reply: copilot.ScreenReply{OK: true}}
	answered := &screenSession{threadID: "th1", projectID: studioView.ProjectID, mailbox: box, emit: out.emit, newID: func() string { return "cmd-1" }, settle: func(id string) { settled = append(settled, id) }}
	if _, err := answered.Run(context.Background(), copilot.ScreenCommand{Name: copilot.ScreenRead}); err != nil {
		t.Fatal(err)
	}
	silent := &screenSession{threadID: "th1", projectID: studioView.ProjectID, mailbox: newMailbox(), emit: func(string, interface{}) {}, newID: func() string { return "cmd-2" }, settle: func(id string) { settled = append(settled, id) }}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _ = silent.Run(ctx, copilot.ScreenCommand{Name: copilot.ScreenRead})
	if len(settled) != 2 || settled[0] != "cmd-1" || settled[1] != "cmd-2" {
		t.Fatalf("answered and timed out commands must both settle, got %v", settled)
	}
}

func TestAStudioTurnSettlesItsScreenCommandsInTheRegistry(t *testing.T) {
	svc := newService(&scriptAI{}, &fakeThreads{}, &fakeMessages{})
	svc.SetScreenMailbox(newMailbox())
	turn, _ := svc.BeginTurn(context.Background(), "th1", "u1")
	holdOpen(turn)
	cc := ownerCtx
	cc.View = studioView
	attached := svc.attachScreen(cc, "th1", turn.Emit)
	screen := attached.Screen.(*screenSession)
	turn.Emit(EventScreenCommand, copilot.ScreenCommand{ID: "cmd-9", Name: copilot.ScreenRead})
	screen.settle("cmd-9")
	replay, _, leave := turn.Subscribe()
	leave()
	if len(replay) != 0 {
		t.Fatalf("the session must settle through the registry, got %v", types(replay))
	}
}
