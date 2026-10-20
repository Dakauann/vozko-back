package conversation_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type finishAccess struct {
	sees    bool
	canSend bool
}

func (a finishAccess) CanAccessEntry(string, string, string, string, bool) bool { return a.sees }

func (a finishAccess) HasWorkspacePermission(_, _, resource, action string, isSystemAdmin bool) bool {
	return isSystemAdmin || (resource == "conversations" && action == "send" && a.canSend)
}

type finishRecorder struct {
	conversation.ConversationStatusUpdater
	calls []conversation.FinishOptions
	err   error
}

func (f *finishRecorder) Finish(_, _ string, opts conversation.FinishOptions) error {
	f.calls = append(f.calls, opts)
	return f.err
}

var finishTarget = shared.EntryRef{EntryID: "e1", EntryType: shared.EntryTypeTelegram}

func TestPersonFinishRefusesWhatTheSingleCloseRefuses(t *testing.T) {
	cases := []struct {
		name   string
		access finishAccess
		entry  shared.EntryRef
		want   error
	}{
		{"cannot see the conversation", finishAccess{sees: false, canSend: true}, finishTarget, conversation.ErrUnauthorized},
		{"lacks conversations:send", finishAccess{sees: true, canSend: false}, finishTarget, conversation.ErrStatusChangeForbidden},
		{"not a conversation channel", finishAccess{sees: true, canSend: true}, shared.EntryRef{EntryID: "e1", EntryType: "fax"}, conversation.ErrUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status := &finishRecorder{}
			err := NewPersonFinish(c.access, status).Finish(context.Background(), shared.Person{UserID: "u1"}, "ws1", c.entry, "")
			if !errors.Is(err, c.want) || len(status.calls) != 0 {
				t.Fatalf("err = %v with %d finishes, want %v and none", err, len(status.calls), c.want)
			}
		})
	}
}

func TestPersonFinishClosesAsAHumanWithTheOutcome(t *testing.T) {
	status := &finishRecorder{}
	err := NewPersonFinish(finishAccess{sees: true, canSend: true}, status).Finish(context.Background(), shared.Person{UserID: "u1"}, "ws1", finishTarget, "sold")
	if err != nil || len(status.calls) != 1 {
		t.Fatalf("err = %v, finishes = %d", err, len(status.calls))
	}
	want := conversation.FinishOptions{Source: conversation.CloseSourceHuman, Reason: conversation.CloseReasonManual, ActorID: "u1", OutcomeCode: "sold"}
	if status.calls[0] != want {
		t.Fatalf("finish = %+v, want %+v", status.calls[0], want)
	}
}

func TestPersonFinishPassesTheOutcomeRuleThrough(t *testing.T) {
	status := &finishRecorder{err: conversation.ErrOutcomeRequired}
	err := NewPersonFinish(finishAccess{sees: true, canSend: true}, status).Finish(context.Background(), shared.Person{UserID: "u1"}, "ws1", finishTarget, "")
	if !errors.Is(err, conversation.ErrOutcomeRequired) {
		t.Fatalf("err = %v, want the status service's outcome refusal", err)
	}
}
