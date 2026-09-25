package copilottools

import (
	"context"
	"testing"
	"time"

	"vozko/domain/balance"
	"vozko/domain/conversation"
	"vozko/domain/copilot"
	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
)

type fakePersonSend struct {
	by    shared.Person
	input conversation.OperatorSendInput
	calls int
	err   error
}

func (f *fakePersonSend) Execute(_ context.Context, by shared.Person, in conversation.OperatorSendInput) (*conversation.Message, error) {
	f.calls++
	f.by, f.input = by, in
	if f.err != nil {
		return nil, f.err
	}
	return &conversation.Message{ID: "m1", Text: in.Text}, nil
}

type fakeScheduler struct {
	by        shared.Person
	scheduled []sm.ScheduleInput
	cancelled []string
	err       error
}

func (f *fakeScheduler) Schedule(_ context.Context, by shared.Person, in sm.ScheduleInput) (*sm.ScheduleResult, error) {
	f.by = by
	f.scheduled = append(f.scheduled, in)
	if f.err != nil {
		return nil, f.err
	}
	return &sm.ScheduleResult{Message: &sm.ScheduledMessage{ID: "s1", ScheduledAt: in.ScheduledAt}}, nil
}

func (f *fakeScheduler) Reschedule(context.Context, shared.Person, sm.RescheduleInput) (*sm.ScheduleResult, error) {
	return nil, nil
}

func (f *fakeScheduler) Cancel(_ context.Context, by shared.Person, workspaceID, id string) error {
	f.by = by
	f.cancelled = append(f.cancelled, workspaceID+"|"+id)
	return f.err
}

type fakeMessageBroadcast struct{ newMessages, entryUpdates int }

func (f *fakeMessageBroadcast) BroadcastNewMessage(string, string, *conversation.Message) {
	f.newMessages++
}
func (f *fakeMessageBroadcast) BroadcastEntryUpdate(string, string, *conversation.Message) {
	f.entryUpdates++
}

func actionDeps(send *fakePersonSend, scheduler *fakeScheduler, broadcast *fakeMessageBroadcast) ConversationActionDeps {
	return ConversationActionDeps{Send: send, Scheduler: scheduler, Entries: &fakeEntries{}, Broadcast: broadcast}
}

func TestConversationActionsNeedApprovalAndConversationSend(t *testing.T) {
	deps := actionDeps(&fakePersonSend{}, &fakeScheduler{}, &fakeMessageBroadcast{})
	for _, tool := range []copilot.Tool{NewSendMessageTool(deps), NewScheduleMessageTool(deps), NewCancelScheduledMessageTool(deps)} {
		if m := tool.Meta(); !m.Mutating || m.Resource != "conversations" || m.Action != "send" {
			t.Fatalf("%s meta = %+v", tool.Definition().Name, m)
		}
	}
}

func TestSendMessageSendsAsTheUserAndShowsItInTheInbox(t *testing.T) {
	send, broadcast := &fakePersonSend{}, &fakeMessageBroadcast{}
	res := NewSendMessageTool(actionDeps(send, &fakeScheduler{}, broadcast)).Execute(context.Background(), member(), map[string]interface{}{
		"entry_id": knownEntry, "entry_type": "whatsapp", "text": "Oi Maria, seu pedido saiu hoje.",
	})
	if res.Status != copilot.StatusOK || send.by.UserID != "u-1" || send.input.WorkspaceID != "ws-1" || send.input.Text != "Oi Maria, seu pedido saiu hoje." {
		t.Fatalf("status %s by %+v input %+v", res.Status, send.by, send.input)
	}
	if broadcast.newMessages != 1 || broadcast.entryUpdates != 1 {
		t.Fatalf("broadcast %+v", broadcast)
	}
}

func TestSendMessageExplainsAClosedWindowAndMoney(t *testing.T) {
	for err, want := range map[error]copilot.Status{
		conversation.ErrWindowClosed:         copilot.StatusError,
		conversation.ErrOutboundWindowClosed: copilot.StatusError,
		balance.ErrInsufficientBalance:       copilot.StatusError,
		conversation.ErrUnauthorized:         copilot.StatusDenied,
	} {
		broadcast := &fakeMessageBroadcast{}
		res := NewSendMessageTool(actionDeps(&fakePersonSend{err: err}, &fakeScheduler{}, broadcast)).Execute(context.Background(), member(), map[string]interface{}{
			"entry_id": knownEntry, "entry_type": "whatsapp", "text": "oi",
		})
		if res.Status != want || res.Message == "falha ao enviar" || broadcast.newMessages != 0 {
			t.Fatalf("%v: %+v", err, res)
		}
	}
}

func TestSendMessageDescribesRecipientAndText(t *testing.T) {
	fields := NewSendMessageTool(actionDeps(&fakePersonSend{}, &fakeScheduler{}, &fakeMessageBroadcast{})).(copilot.Describer).Describe(context.Background(), member(), map[string]interface{}{
		"entry_id": knownEntry, "entry_type": "whatsapp", "text": "Oi Maria",
	})
	got := map[string]string{}
	for _, f := range fields {
		got[f.Key] = f.Value
	}
	if _, duplicated := got["text"]; got["conversation"] != "Maria (••••9624)" || duplicated {
		t.Fatalf("fields = %v", fields)
	}
}

func TestScheduleMessageSchedulesAsTheUser(t *testing.T) {
	scheduler := &fakeScheduler{}
	res := NewScheduleMessageTool(actionDeps(&fakePersonSend{}, scheduler, &fakeMessageBroadcast{})).Execute(context.Background(), member(), map[string]interface{}{
		"entry_id": knownEntry, "entry_type": "whatsapp", "text": "Bom dia!", "scheduled_at": "2026-09-26T09:00:00-03:00",
	})
	if res.Status != copilot.StatusOK || scheduler.by.UserID != "u-1" || len(scheduler.scheduled) != 1 {
		t.Fatalf("status %s: %s", res.Status, res.Message)
	}
	if got := scheduler.scheduled[0].ScheduledAt.UTC().Format(time.RFC3339); got != "2026-09-26T12:00:00Z" {
		t.Fatalf("scheduled at %s", got)
	}
}

func TestScheduleMessageNeedsAnExplicitTimeZone(t *testing.T) {
	scheduler := &fakeScheduler{}
	res := NewScheduleMessageTool(actionDeps(&fakePersonSend{}, scheduler, &fakeMessageBroadcast{})).Execute(context.Background(), member(), map[string]interface{}{
		"entry_id": knownEntry, "entry_type": "whatsapp", "text": "x", "scheduled_at": "amanhã às 9",
	})
	if res.Status != copilot.StatusError || len(scheduler.scheduled) != 0 {
		t.Fatalf("status %s scheduled %v", res.Status, scheduler.scheduled)
	}
}

func TestScheduleMessageExplainsTheWindowRules(t *testing.T) {
	for _, err := range []error{sm.ErrWindowClosed, sm.ErrScheduledAtPastWindow, sm.ErrScheduledAtTooSoon, sm.ErrScheduledAtTooFar} {
		res := NewScheduleMessageTool(actionDeps(&fakePersonSend{}, &fakeScheduler{err: err}, &fakeMessageBroadcast{})).Execute(context.Background(), member(), map[string]interface{}{
			"entry_id": knownEntry, "entry_type": "whatsapp", "text": "x", "scheduled_at": "2026-09-26T09:00:00-03:00",
		})
		if res.Status != copilot.StatusError || res.Message == "falha ao agendar" {
			t.Fatalf("%v: %+v", err, res)
		}
	}
}

func TestCancelScheduledMessageCancelsAsTheUser(t *testing.T) {
	scheduler := &fakeScheduler{}
	res := NewCancelScheduledMessageTool(actionDeps(&fakePersonSend{}, scheduler, &fakeMessageBroadcast{})).Execute(context.Background(), member(), map[string]interface{}{
		"scheduled_message_id": "7f6e5d4c-3b2a-4190-8f7e-6d5c4b3a2f1e",
	})
	if res.Status != copilot.StatusOK || scheduler.by.UserID != "u-1" || scheduler.cancelled[0] != "ws-1|7f6e5d4c-3b2a-4190-8f7e-6d5c4b3a2f1e" {
		t.Fatalf("status %s cancelled %v", res.Status, scheduler.cancelled)
	}
}
