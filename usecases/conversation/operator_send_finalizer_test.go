package conversation_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	"vozko/domain/shared"
)

// These pin the five side effects a delivered operator reply owes its
// conversation. They used to live on the WebSocket hub, where nothing tested
// them and no other send surface ran them at all.

type recordedTransition struct {
	entryID, entryType string
	msgType            conversation.MessageType
	direction          conversation.MessageHistoryDirection
}

type fakeStatusUpdater struct {
	transitions []recordedTransition
	err         error
}

func (f *fakeStatusUpdater) GetConversationStatus(string, string) conversation.ConversationStatus {
	return ""
}
func (f *fakeStatusUpdater) SetConversationStatus(string, string, conversation.ConversationStatus) error {
	return nil
}
func (f *fakeStatusUpdater) Finish(string, string, conversation.FinishOptions) error { return nil }
func (f *fakeStatusUpdater) TransitionOnMessage(entryID, entryType string, msgType conversation.MessageType, direction conversation.MessageHistoryDirection) error {
	f.transitions = append(f.transitions, recordedTransition{entryID, entryType, msgType, direction})
	return f.err
}
func (f *fakeStatusUpdater) GetStatusCounts(string, string, string) (map[string]int64, error) {
	return nil, nil
}

type fakeWorkspaceResolver struct {
	workspaceID string
	campaignID  string
	err         error
}

func (f *fakeWorkspaceResolver) GetCampaignWorkspaceID(string, string) (string, error) {
	return f.workspaceID, f.err
}
func (f *fakeWorkspaceResolver) GetCampaignDepartmentID(string, string) (string, error) {
	return "", nil
}
func (f *fakeWorkspaceResolver) GetEntryWorkspaceID(string, string) (string, error) {
	return f.workspaceID, f.err
}
func (f *fakeWorkspaceResolver) GetEntryDepartmentID(string, string) (string, error) {
	return "", nil
}
func (f *fakeWorkspaceResolver) GetEntryCampaignID(string, string) (string, error) {
	return f.campaignID, f.err
}

type fakeEventLogger struct{ events []*ce.ConversationEvent }

func (f *fakeEventLogger) Log(e *ce.ConversationEvent) { f.events = append(f.events, e) }

type endedSession struct {
	workspaceID, entryID, entryType, outcome, reason, handoffUserID string
}

type fakeAISessionEnder struct{ ended []endedSession }

func (f *fakeAISessionEnder) EndOpenRaw(workspaceID, entryID, entryType, outcome, reason, handoffUserID string) {
	f.ended = append(f.ended, endedSession{workspaceID, entryID, entryType, outcome, reason, handoffUserID})
}

type assignedStage struct {
	workspaceID, campaignID, campaignType, entryID, entryType string
}

type fakeInitialStageAssigner struct{ assigned []assignedStage }

func (f *fakeInitialStageAssigner) AutoAssignInitialStage(workspaceID, campaignID, campaignType, entryID, entryType string) {
	f.assigned = append(f.assigned, assignedStage{workspaceID, campaignID, campaignType, entryID, entryType})
}

type finalizerFixture struct {
	status    *fakeStatusUpdater
	resolver  *fakeWorkspaceResolver
	events    *fakeEventLogger
	ai        *fakeAISessionEnder
	stages    *fakeInitialStageAssigner
	finalizer conversation.OperatorSendFinalizer
}

func newFinalizerFixture(t *testing.T) *finalizerFixture {
	t.Helper()
	f := &finalizerFixture{
		status:   &fakeStatusUpdater{},
		resolver: &fakeWorkspaceResolver{workspaceID: "ws-1", campaignID: "camp-1"},
		events:   &fakeEventLogger{},
		ai:       &fakeAISessionEnder{},
		stages:   &fakeInitialStageAssigner{},
	}
	finalizer, err := NewOperatorSendFinalizer(f.status, f.resolver, f.events, f.ai, f.stages)
	if err != nil {
		t.Fatalf("NewOperatorSendFinalizer: %v", err)
	}
	f.finalizer = finalizer
	return f
}

func operatorMessage() *conversation.Message {
	return &conversation.Message{
		ID:          "msg-1",
		EntryID:     "entry-1",
		EntryType:   shared.EntryTypeTelegram,
		MessageType: conversation.MessageTypeOperator,
	}
}

func TestFinalizeOperatorSendRunsEveryStep(t *testing.T) {
	f := newFinalizerFixture(t)

	err := f.finalizer.FinalizeOperatorSend(context.Background(), conversation.FinalizeOperatorSendInput{
		EntryID:     "entry-1",
		EntryType:   string(shared.EntryTypeTelegram),
		WorkspaceID: "ws-hint",
		ActorUserID: "user-1",
		Message:     operatorMessage(),
	})
	if err != nil {
		t.Fatalf("FinalizeOperatorSend: %v", err)
	}

	if len(f.status.transitions) != 1 {
		t.Fatalf("status transitions = %d, want 1", len(f.status.transitions))
	}
	if got := f.status.transitions[0]; got.entryID != "entry-1" || got.msgType != conversation.MessageTypeOperator {
		t.Errorf("transition = %+v", got)
	}

	if len(f.events.events) != 1 {
		t.Fatalf("events logged = %d, want 1", len(f.events.events))
	}
	ev := f.events.events[0]
	if ev.EventType != ce.EventReplied {
		t.Errorf("event type = %q, want %q", ev.EventType, ce.EventReplied)
	}
	// The channel used to come from a switch that defaulted to "whatsapp", so an
	// operator's Telegram reply was filed as a WhatsApp event.
	if ev.Channel != string(shared.EntryTypeTelegram) {
		t.Errorf("event channel = %q, want %q", ev.Channel, shared.EntryTypeTelegram)
	}
	if ev.WorkspaceID != "ws-1" {
		t.Errorf("event workspace = %q, want the RESOLVED workspace, not the caller's hint", ev.WorkspaceID)
	}

	if len(f.ai.ended) != 1 || f.ai.ended[0].reason != "human_reply" {
		t.Errorf("ai sessions ended = %+v, want one handed_off/human_reply", f.ai.ended)
	}
	if len(f.stages.assigned) != 1 || f.stages.assigned[0].campaignID != "camp-1" {
		t.Errorf("initial stage assignments = %+v", f.stages.assigned)
	}
}

// The caller's workspace hint is the connection's workspace, which is wrong for
// a platform admin working across workspaces. The resolver wins when it answers.
func TestFinalizeOperatorSendFallsBackToTheHintWhenResolutionFails(t *testing.T) {
	f := newFinalizerFixture(t)
	f.resolver.workspaceID = ""
	f.resolver.err = errors.New("no workspace")

	_ = f.finalizer.FinalizeOperatorSend(context.Background(), conversation.FinalizeOperatorSendInput{
		EntryID:     "entry-1",
		EntryType:   string(shared.EntryTypeWhatsApp),
		WorkspaceID: "ws-hint",
		ActorUserID: "user-1",
		Message:     operatorMessage(),
	})

	if len(f.events.events) != 1 || f.events.events[0].WorkspaceID != "ws-hint" {
		t.Errorf("event workspace = %+v, want the caller's hint", f.events.events)
	}
	// No campaign resolvable means no stage, and that must not stop the rest.
	if len(f.stages.assigned) != 0 {
		t.Errorf("stage assigned despite an unresolvable campaign: %+v", f.stages.assigned)
	}
}

// A conversation must never lose a delivered message because a side effect
// failed. Every step degrades to a log line.
func TestFinalizeOperatorSendSurvivesAFailingStep(t *testing.T) {
	f := newFinalizerFixture(t)
	f.status.err = errors.New("status store down")

	if err := f.finalizer.FinalizeOperatorSend(context.Background(), conversation.FinalizeOperatorSendInput{
		EntryID:     "entry-1",
		EntryType:   string(shared.EntryTypeWhatsApp),
		ActorUserID: "user-1",
		Message:     operatorMessage(),
	}); err != nil {
		t.Fatalf("a failing transition must not fail the finalize: %v", err)
	}
	if len(f.events.events) != 1 || len(f.stages.assigned) != 1 {
		t.Errorf("later steps were skipped after an earlier failure")
	}
}

// A missing dependency stops the boot. The alternative — nil-checking each step
// at call time — is what let a channel silently lose its board cards and its
// timeline for months.
func TestNewOperatorSendFinalizerRefusesMissingDependencies(t *testing.T) {
	full := func() (conversation.ConversationStatusUpdater, conversation.CampaignWorkspaceResolver, ce.Logger, AISessionEnder, conversation.InitialStageAssigner) {
		return &fakeStatusUpdater{}, &fakeWorkspaceResolver{}, &fakeEventLogger{}, &fakeAISessionEnder{}, &fakeInitialStageAssigner{}
	}

	if _, err := NewOperatorSendFinalizer(nil, nil, nil, nil, nil); err == nil {
		t.Error("an entirely unwired finalizer was accepted")
	}

	s, r, e, a, i := full()
	if _, err := NewOperatorSendFinalizer(s, r, e, a, nil); err == nil {
		t.Error("a nil initial stage assigner was accepted: entries would silently never reach the board")
	}
	s, r, e, a, i = full()
	if _, err := NewOperatorSendFinalizer(s, r, nil, a, i); err == nil {
		t.Error("a nil event logger was accepted: replies would silently never reach the timeline")
	}
}

func TestFinalizeOperatorSendRejectsAnEmptyEntry(t *testing.T) {
	f := newFinalizerFixture(t)
	err := f.finalizer.FinalizeOperatorSend(context.Background(), conversation.FinalizeOperatorSendInput{
		EntryType: string(shared.EntryTypeWhatsApp),
		Message:   operatorMessage(),
	})
	if !errors.Is(err, conversation.ErrEntryIDRequired) {
		t.Fatalf("err = %v, want ErrEntryIDRequired", err)
	}
	if len(f.status.transitions) != 0 {
		t.Errorf("side effects ran for an entry-less input")
	}
}
