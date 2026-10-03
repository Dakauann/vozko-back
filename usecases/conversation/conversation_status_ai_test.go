package conversation_usecase

import (
	"testing"

	aa "vozko/domain/ai_attendance"
)

type endSpy struct {
	calls int
	last  aa.EndRequest
}

func (e *endSpy) End(request aa.EndRequest) {
	e.calls++
	e.last = request
}

func TestEndAISessionContainedOnFinished(t *testing.T) {
	svc := &ConversationStatusService{}
	spy := &endSpy{}
	svc.SetAISessionEnder(spy)
	svc.endAISessionContained("entry-1", "whatsapp", "ws-1", "closer-1")
	if spy.calls != 1 {
		t.Fatalf("calls=%d", spy.calls)
	}
	if spy.last.Outcome != aa.OutcomeContained || spy.last.Reason != aa.EndReasonConversationFinished {
		t.Fatalf("last=%+v", spy.last)
	}
	if spy.last.WorkspaceID != "ws-1" || spy.last.EntryID != "entry-1" || spy.last.EndedBy != "closer-1" {
		t.Fatalf("last=%+v: the session end must name who finished the conversation", spy.last)
	}
}

func TestEndAISessionContained_NoWorkspaceNoop(t *testing.T) {
	svc := &ConversationStatusService{}
	spy := &endSpy{}
	svc.SetAISessionEnder(spy)
	svc.endAISessionContained("entry-1", "whatsapp", "", "closer-1")
	if spy.calls != 0 {
		t.Fatalf("expected noop without workspace, calls=%d", spy.calls)
	}
}
