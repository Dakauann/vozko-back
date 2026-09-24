package conversation_usecase

import (
	"testing"

	"vozko/domain/actor"
	"vozko/domain/conversation"
	conv_event "vozko/domain/conversation_event"
	"vozko/domain/shared"
)

type recordedEvents struct {
	events []*conv_event.ConversationEvent
}

func (r *recordedEvents) Log(ev *conv_event.ConversationEvent) { r.events = append(r.events, ev) }

func (r *recordedEvents) finished(t *testing.T) *conv_event.ConversationEvent {
	t.Helper()
	for _, ev := range r.events {
		if ev.EventType == conv_event.EventFinished {
			return ev
		}
	}
	t.Fatalf("no finished event among %d", len(r.events))
	return nil
}

// The timeline is where a finish is read back: who closed the conversation and
// with which outcome. A person, an agent and a workflow are recorded alike, the
// outcome with the label it had when it was chosen.
func TestFinishRecordsTheOutcomeAndWhoClosedOnTheTimeline(t *testing.T) {
	for _, tc := range []struct {
		name      string
		opts      conversation.FinishOptions
		wantActor string
		wantKind  actor.Kind
	}{
		{"person", conversation.FinishOptions{Source: conversation.CloseSourceHuman, ActorID: "11111111-1111-4111-8111-111111111111"},
			"11111111-1111-4111-8111-111111111111", actor.KindHuman},
		{"agent", conversation.FinishOptions{Source: conversation.CloseSourceAI, ActorID: actor.FormatAI("agent-1")},
			actor.FormatAI("agent-1"), actor.KindAI},
		{"workflow", conversation.FinishOptions{Source: conversation.CloseSourceSystem, Reason: conversation.CloseReasonWorkflow, ActorID: actor.FormatWorkflow("wf-1")},
			actor.FormatWorkflow("wf-1"), actor.KindWorkflow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusOngoing)}
			svc := outcomeServiceWith(t, repo, &stubCaptureReader{capture: requiringCapture()})
			events := &recordedEvents{}
			svc.SetEventLogger(events)

			opts := tc.opts
			opts.OutcomeCode = "sale"
			if err := svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), opts); err != nil {
				t.Fatalf("Finish: %v", err)
			}

			ev := events.finished(t)
			details := ev.DetailsMap()
			if details["close_outcome"] != "sale" || details["close_outcome_label"] != "Venda fechada" {
				t.Errorf("outcome on the timeline = %q (%q), want sale (Venda fechada)", details["close_outcome"], details["close_outcome_label"])
			}
			if ev.ActorID != tc.wantActor || ev.ActorKind != tc.wantKind {
				t.Errorf("closed by %q (%s), want %q (%s)", ev.ActorID, ev.ActorKind, tc.wantActor, tc.wantKind)
			}
		})
	}
}

func TestFinishWithoutAnOutcomeLeavesItOffTheTimeline(t *testing.T) {
	repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusOngoing)}
	svc := outcomeServiceWith(t, repo, nil)
	events := &recordedEvents{}
	svc.SetEventLogger(events)

	if err := svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{Source: conversation.CloseSourceHuman}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	details := events.finished(t).DetailsMap()
	if _, ok := details["close_outcome"]; ok {
		t.Errorf("recorded an outcome nobody chose: %v", details)
	}
}

type announcedStatus struct {
	entryID string
	status  conversation.ConversationStatus
	close   conversation.CloseRecord
}

type recordedAnnouncements struct{ got []announcedStatus }

func (r *recordedAnnouncements) AnnounceStatus(entryID, _ string, status conversation.ConversationStatus, close conversation.CloseRecord) {
	r.got = append(r.got, announcedStatus{entryID: entryID, status: status, close: close})
}

// Open screens learn of a finish from the service every finish goes through,
// whoever closed: a person, an agent, a workflow or the idle sweep.
func TestFinishIsAnnouncedWithHowItWasClosed(t *testing.T) {
	repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusOngoing)}
	svc := outcomeServiceWith(t, repo, &stubCaptureReader{capture: requiringCapture()})
	announced := &recordedAnnouncements{}
	svc.SetStatusAnnouncer(announced)

	err := svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{
		Source: conversation.CloseSourceAI, ActorID: actor.FormatAI("agent-1"), OutcomeCode: "sale",
	})
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if len(announced.got) != 1 {
		t.Fatalf("announced %d times, want once", len(announced.got))
	}
	got := announced.got[0]
	if got.status != conversation.ConversationStatusFinished || got.close.Source != conversation.CloseSourceAI ||
		got.close.Reason != conversation.CloseReasonAIResolved || got.close.Outcome != "sale" || got.close.ClosedAt == nil {
		t.Errorf("announced %+v, want finished by ai (ai_resolved) with sale and a close time", got)
	}

	if err := svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{
		Source: conversation.CloseSourceAI, OutcomeCode: "sale",
	}); err != nil {
		t.Fatalf("second Finish: %v", err)
	}
	if len(announced.got) != 1 {
		t.Errorf("finishing a finished conversation announced again")
	}
}

func TestARefusedFinishIsNotAnnounced(t *testing.T) {
	repo := &outcomeEntryRepo{status: string(conversation.ConversationStatusOngoing)}
	svc := outcomeServiceWith(t, repo, &stubCaptureReader{capture: requiringCapture()})
	announced := &recordedAnnouncements{}
	svc.SetStatusAnnouncer(announced)

	_ = svc.Finish("entry-1", string(shared.EntryTypeWhatsApp), conversation.FinishOptions{Source: conversation.CloseSourceAI})
	if len(announced.got) != 0 {
		t.Errorf("announced a finish that was refused: %+v", announced.got)
	}
}
