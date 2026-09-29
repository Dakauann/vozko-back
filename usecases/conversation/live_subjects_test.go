package conversation_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/livedecision"
	"vozko/domain/shared"
	livedecisions_usecase "vozko/usecases/livedecisions"
)

type liveQueue struct{ refs []livedecision.EntryRef }

func (q *liveQueue) Queue(ref livedecision.EntryRef) { q.refs = append(q.refs, ref) }

type liveMode bool

func (m liveMode) Acts(context.Context, string) bool { return bool(m) }

func TestSchedulingAnAnalysisAlsoQueuesTheLiveDecision(t *testing.T) {
	state := &analysisState{fields: map[string]string{}}
	live := &liveQueue{}
	NewAnalysisScheduler(state, live).ScheduleAnalysis("entry-1", shared.EntryTypeTelegram)

	if _, ok := decodeAnalysisDebounceValue(state.fields["entry-1"]); !ok {
		t.Fatal("the five-minute run is still scheduled")
	}
	if len(live.refs) != 1 || live.refs[0] != (livedecision.EntryRef{EntryID: "entry-1", EntryType: string(shared.EntryTypeTelegram)}) {
		t.Fatalf("queued %+v", live.refs)
	}
}

func TestSchedulingWithoutAnEntryQueuesNothing(t *testing.T) {
	state := &analysisState{fields: map[string]string{}}
	live := &liveQueue{}
	scheduler := NewAnalysisScheduler(state, live)
	scheduler.ScheduleAnalysis("", shared.EntryTypeTelegram)
	scheduler.ScheduleAnalysis("entry-1", "")
	var unwired *AnalysisScheduler
	unwired.ScheduleAnalysis("entry-1", shared.EntryTypeTelegram)

	if len(state.fields) != 0 || len(live.refs) != 0 {
		t.Fatalf("stamps=%d queued=%d", len(state.fields), len(live.refs))
	}
}

func TestSchedulingWithoutLiveDecisionsStillStampsTheAnalysis(t *testing.T) {
	state := &analysisState{fields: map[string]string{}}
	NewAnalysisScheduler(state, nil).ScheduleAnalysis("entry-1", shared.EntryTypeWhatsApp)
	if len(state.fields) != 1 {
		t.Fatal("the analysis does not depend on live decisions")
	}
}

func TestLiveSubjectsCarryTheChannelSwitches(t *testing.T) {
	subjects := NewLiveSubjects()
	subjects.SetAnalysisSubjectResolver(shared.EntryTypeInstagram, func(_ context.Context, entryID string) (*AnalysisSubject, error) {
		return &AnalysisSubject{EntryID: entryID, WorkspaceID: "ws-1", EnableAutoStaging: true}, nil
	})

	trigger, found, err := subjects.Subject(context.Background(), livedecision.EntryRef{EntryID: "entry-1", EntryType: string(shared.EntryTypeInstagram)})

	want := livedecisions_usecase.Trigger{
		WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: shared.EntryTypeInstagram,
		Features: livedecision.Features{Staging: true},
	}
	if err != nil || !found || trigger != want {
		t.Fatalf("trigger=%+v found=%v err=%v", trigger, found, err)
	}
}

func TestLiveSubjectsForAnUnknownConversationAreNotFound(t *testing.T) {
	subjects := NewLiveSubjects()
	subjects.SetAnalysisSubjectResolver(shared.EntryTypeWhatsApp, func(context.Context, string) (*AnalysisSubject, error) {
		return nil, nil
	})
	_, found, err := subjects.Subject(context.Background(), livedecision.EntryRef{EntryID: "gone", EntryType: string(shared.EntryTypeWhatsApp)})
	if err != nil || found {
		t.Fatalf("found=%v err=%v", found, err)
	}
}

func TestLiveSubjectsFailClosed(t *testing.T) {
	subjects := NewLiveSubjects()
	subjects.SetAnalysisSubjectResolver(shared.EntryTypeWhatsApp, func(context.Context, string) (*AnalysisSubject, error) {
		return nil, errors.New("db down")
	})
	if _, found, err := subjects.Subject(context.Background(), livedecision.EntryRef{EntryID: "e", EntryType: string(shared.EntryTypeWhatsApp)}); err == nil || found {
		t.Fatal("an unreadable conversation is an error, never a default")
	}
	if _, found, err := subjects.Subject(context.Background(), livedecision.EntryRef{EntryID: "e", EntryType: string(shared.EntryTypeTelegram)}); err == nil || found {
		t.Fatal("a channel without a resolver is an error, never a default")
	}
}

func TestLiveModeIsReadThroughTheGate(t *testing.T) {
	if (&handleWhatsAppMessageUseCase{}).liveActs("ws-1") {
		t.Fatal("no gate, no live mode")
	}
	if !(&handleWhatsAppMessageUseCase{live: liveMode(true)}).liveActs("ws-1") {
		t.Fatal("the gate says whether decisions run live")
	}
}
