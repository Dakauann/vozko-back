package conversation_usecase

import "testing"

type endSpy struct {
	calls int
	last  struct {
		ws, entry, typ, outcome, reason string
	}
}

func (e *endSpy) EndOpenRaw(workspaceID, entryID, entryType, outcome, reason, handoffUserID string) {
	e.calls++
	e.last.ws = workspaceID
	e.last.entry = entryID
	e.last.typ = entryType
	e.last.outcome = outcome
	e.last.reason = reason
}

func TestEndAISessionContainedOnFinished(t *testing.T) {
	svc := &ConversationStatusService{}
	spy := &endSpy{}
	svc.SetAISessionEnder(spy)
	svc.endAISessionContained("entry-1", "whatsapp", "ws-1")
	if spy.calls != 1 {
		t.Fatalf("calls=%d", spy.calls)
	}
	if spy.last.outcome != "contained" || spy.last.reason != "conversation_finished" {
		t.Fatalf("last=%+v", spy.last)
	}
	if spy.last.ws != "ws-1" || spy.last.entry != "entry-1" {
		t.Fatalf("last=%+v", spy.last)
	}
}

func TestEndAISessionContained_NoWorkspaceNoop(t *testing.T) {
	svc := &ConversationStatusService{}
	spy := &endSpy{}
	svc.SetAISessionEnder(spy)
	svc.endAISessionContained("entry-1", "whatsapp", "")
	if spy.calls != 0 {
		t.Fatalf("expected noop without workspace, calls=%d", spy.calls)
	}
}
