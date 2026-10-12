package copilot_usecase

import (
	"context"
	"strings"
	"testing"
	"time"

	"vozko/domain/ai"
	"vozko/domain/aichat"
	"vozko/domain/copilot"
	"vozko/usecases/agentloop"
)

type listedMessages struct {
	fakeMessages
	asked aichat.ListMessagesInput
}

func (l *listedMessages) ListByThread(in aichat.ListMessagesInput) ([]*aichat.Message, int64, error) {
	l.asked = in
	return l.fakeMessages.ListByThread(in)
}

func TestTheNextReplyStartsFromTheThreadCheckpoint(t *testing.T) {
	at := time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC)
	thread := testThread()
	thread.Checkpoint, thread.CheckpointAt = "<conversation-checkpoint>\nresumo\n</conversation-checkpoint>", &at
	ms := &listedMessages{fakeMessages: fakeMessages{list: []*aichat.Message{{Role: aichat.RoleUser, Content: "e agora?"}}}}
	svc := NewService(agentloop.Engine{AI: &scriptAI{}}, NewRegistry(), &fakeAccess{}, openFunds{}, &fakeThreads{thread: thread}, ms, nil, nil, func() string { return "a" })
	h, err := svc.buildHistory("t1", "m", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 2 || h[0].Role != ai.RoleUser || h[0].Content != thread.Checkpoint || !strings.Contains(h[1].Content, "e agora?") {
		t.Fatalf("history = %+v, want the checkpoint then what came after it", h)
	}
	if ms.asked.After == nil || !ms.asked.After.Equal(at) || ms.asked.Limit != 0 {
		t.Fatalf("listed %+v, want every message after the checkpoint", ms.asked)
	}
}

func TestWithoutACheckpointTheWholeThreadIsLoaded(t *testing.T) {
	ms := &listedMessages{}
	svc := NewService(agentloop.Engine{AI: &scriptAI{}}, NewRegistry(), &fakeAccess{}, openFunds{}, &fakeThreads{thread: testThread()}, ms, nil, nil, func() string { return "a" })
	if _, err := svc.buildHistory("t1", "m", true); err != nil {
		t.Fatal(err)
	}
	if ms.asked.After != nil || ms.asked.Limit != 0 || ms.asked.Offset != 0 {
		t.Fatalf("listed %+v, want the whole thread", ms.asked)
	}
}

func TestACheckpointIsKeptOnTheThread(t *testing.T) {
	threads := &fakeThreads{thread: testThread()}
	svc := newService(&scriptAI{}, threads, &fakeMessages{})
	if err := svc.checkpointer("t1")("resumo"); err != nil {
		t.Fatal(err)
	}
	if threads.checkpoint != "resumo" || threads.checkpointAt.IsZero() {
		t.Fatalf("checkpoint %q at %v", threads.checkpoint, threads.checkpointAt)
	}
}

func TestRepeatingTheSameCallAsksTheUserHowToGoOn(t *testing.T) {
	th := &fakeThreads{thread: testThread()}
	ms := &fakeMessages{}
	rt := &fakeTool{name: "list_agents", meta: readMeta}
	same := []ai.ToolCall{call("list_agents", map[string]interface{}{"page": 1})}
	prov := &scriptAI{turns: [][]ai.ToolCall{same, same, same, same}}
	cp := &capture{}
	if err := newService(prov, th, ms, rt).Stream(context.Background(), th.thread, copilot.UserMessage{Content: "liste"}, ownerCtx, cp.emit); err != nil {
		t.Fatal(err)
	}
	if rt.calls != 3 {
		t.Fatalf("ran %d times, want the question on the third identical call", rt.calls)
	}
	stored := string(ms.last().ToolCalls)
	if !strings.Contains(stored, `"kind":"ask"`) || !strings.Contains(stored, "list_agents") {
		t.Fatalf("the reply must end with a question card naming the repeated action, got %s", stored)
	}
	if cp.has("error") {
		t.Fatal("a repeated action asks the user; it is not shown as an error")
	}
}
