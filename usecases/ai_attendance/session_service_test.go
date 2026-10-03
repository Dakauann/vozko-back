package ai_attendance_usecase

import (
	"testing"
	"time"

	"vozko/domain/actor"
	aa "vozko/domain/ai_attendance"
	ce "vozko/domain/conversation_event"
)

type memRepo struct {
	byEntry map[string]*aa.Session
	byID    map[string]*aa.Session
}

func newMem() *memRepo {
	return &memRepo{byEntry: map[string]*aa.Session{}, byID: map[string]*aa.Session{}}
}

func key(ws, entry, typ string) string { return ws + "|" + entry + "|" + typ }

func (m *memRepo) Create(s *aa.Session) error {
	cp := *s
	m.byID[s.ID] = &cp
	if s.EndedAt == nil {
		m.byEntry[key(s.WorkspaceID, s.EntryID, s.EntryType)] = &cp
	}
	return nil
}

func (m *memRepo) Update(s *aa.Session) error {
	cp := *s
	m.byID[s.ID] = &cp
	k := key(s.WorkspaceID, s.EntryID, s.EntryType)
	if s.EndedAt == nil {
		m.byEntry[k] = &cp
	} else {
		delete(m.byEntry, k)
	}
	return nil
}

func (m *memRepo) FindOpenByEntry(ws, entry, typ string) (*aa.Session, error) {
	s := m.byEntry[key(ws, entry, typ)]
	if s == nil {
		return nil, nil
	}
	cp := *s
	return &cp, nil
}

func (m *memRepo) FindOpenByCallID(ws, callID string) (*aa.Session, error) {
	for _, s := range m.byID {
		if s.WorkspaceID == ws && s.CallID == callID && s.EndedAt == nil {
			cp := *s
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *memRepo) ExistsByCallID(ws, callID string) (bool, error) {
	for _, s := range m.byID {
		if s.CallID == callID && (ws == "" || s.WorkspaceID == ws) {
			return true, nil
		}
	}
	return false, nil
}

func (m *memRepo) BackfillFromCDR(string, bool, int) (int64, error) { return 0, nil }

func (m *memRepo) FindByID(id string) (*aa.Session, error) {
	s := m.byID[id]
	if s == nil {
		return nil, aa.ErrSessionNotFound
	}
	cp := *s
	return &cp, nil
}

func (m *memRepo) ListByEntry(string, string, string, int, int) ([]*aa.Session, int64, error) {
	return nil, 0, nil
}

func (m *memRepo) Stats(string, *time.Time, *time.Time) (*aa.Stats, error) {
	return &aa.Stats{}, nil
}

type fakeLog struct {
	events []*ce.ConversationEvent
}

func (f *fakeLog) Log(e *ce.ConversationEvent) {
	f.events = append(f.events, e)
}

func TestEnsureOpenAndHandoff(t *testing.T) {
	repo := newMem()
	fl := &fakeLog{}
	svc := NewSessionService(repo, fl)

	s1 := svc.EnsureOpen(aa.StartInput{
		WorkspaceID: "ws", EntryID: "e1", EntryType: "whatsapp",
		AgentID: "ag1", Channel: "whatsapp",
	})
	if s1 == nil || !s1.IsOpen() {
		t.Fatal("expected open session")
	}
	s2 := svc.EnsureOpen(aa.StartInput{
		WorkspaceID: "ws", EntryID: "e1", EntryType: "whatsapp",
		AgentID: "ag1", Channel: "whatsapp",
	})
	if s2.ID != s1.ID {
		t.Fatal("should reuse open session")
	}

	svc.RecordAIReply(aa.StartInput{
		WorkspaceID: "ws", EntryID: "e1", EntryType: "whatsapp",
		AgentID: "ag1", Channel: "whatsapp",
	}, "msg-1")

	open, _ := repo.FindOpenByEntry("ws", "e1", "whatsapp")
	if open == nil || open.AIMessageCount != 1 {
		t.Fatalf("ai count=%v", open)
	}

	svc.End(aa.EndRequest{WorkspaceID: "ws", EntryID: "e1", EntryType: "whatsapp", Outcome: aa.OutcomeHandedOff, Reason: aa.EndReasonHumanReply, HandoffTo: "user-9", EndedBy: "user-9"})
	open, _ = repo.FindOpenByEntry("ws", "e1", "whatsapp")
	if open != nil {
		t.Fatal("expected closed")
	}
	ended := repo.byID[s1.ID]
	if ended.Outcome != aa.OutcomeHandedOff || ended.HandoffTargetUserID != "user-9" {
		t.Fatalf("ended=%+v", ended)
	}

	svc.End(aa.EndRequest{WorkspaceID: "ws", EntryID: "e1", EntryType: "whatsapp", Outcome: aa.OutcomeContained, Reason: "x"})

	var hasStart, hasReply, hasEnd bool
	for _, e := range fl.events {
		switch e.EventType {
		case ce.EventAISessionStarted:
			hasStart = true
		case ce.EventAIReplied:
			hasReply = true
		case ce.EventAISessionEnded:
			hasEnd = true
		}
	}
	if !hasStart || !hasReply || !hasEnd {
		t.Fatalf("events incomplete: start=%v reply=%v end=%v n=%d", hasStart, hasReply, hasEnd, len(fl.events))
	}
}

func TestEndOpenByCallIDFallback(t *testing.T) {
	repo := newMem()
	svc := NewSessionService(repo, nil)

	s := svc.EnsureOpen(aa.StartInput{
		WorkspaceID: "ws", EntryID: "entry-uuid", EntryType: "voice",
		AgentID: "ag1", Channel: "voice", CallID: "sip-call-xyz",
	})
	if s == nil {
		t.Fatal("expected session")
	}
	svc.End(aa.EndRequest{WorkspaceID: "ws", EntryType: "voice", CallID: "sip-call-xyz", Outcome: aa.OutcomeHandedOff, Reason: "transfer_completed", HandoffTo: "user-1"})
	open, _ := repo.FindOpenByEntry("ws", "entry-uuid", "voice")
	if open != nil {
		t.Fatal("expected closed via call_id fallback")
	}
	ended := repo.byID[s.ID]
	if ended == nil || ended.Outcome != aa.OutcomeHandedOff {
		t.Fatalf("ended=%+v", ended)
	}
}

func TestContainedFinishWhatsApp(t *testing.T) {
	repo := newMem()
	svc := NewSessionService(repo, nil)
	svc.EnsureOpen(aa.StartInput{
		WorkspaceID: "ws", EntryID: "e2", EntryType: "whatsapp",
		AgentID: "ag1", Channel: "whatsapp",
	})
	svc.End(aa.EndRequest{WorkspaceID: "ws", EntryID: "e2", EntryType: "whatsapp", Outcome: aa.OutcomeContained, Reason: aa.EndReasonConversationFinished})
	open, _ := repo.FindOpenByEntry("ws", "e2", "whatsapp")
	if open != nil {
		t.Fatal("expected contained close")
	}
}

func endedEvent(t *testing.T, fl *fakeLog) *ce.ConversationEvent {
	t.Helper()
	for _, e := range fl.events {
		if e.EventType == ce.EventAISessionEnded {
			return e
		}
	}
	t.Fatal("no ai_session_ended event")
	return nil
}

func TestTheSessionEndIsCreditedToWhoeverEndedIt(t *testing.T) {
	cases := map[string]struct {
		endedBy string
		kind    actor.Kind
		id      string
	}{
		"a person":        {"user-9", actor.KindHuman, "user-9"},
		"the agent":       {"ai:ag1", actor.KindAI, "ag1"},
		"a workflow":      {"workflow:wf1", actor.KindWorkflow, "wf1"},
		"nobody in doubt": {"", actor.KindSystem, ""},
	}
	for name, tc := range cases {
		repo := newMem()
		fl := &fakeLog{}
		svc := NewSessionService(repo, fl)
		svc.EnsureOpen(aa.StartInput{WorkspaceID: "ws", EntryID: "e1", EntryType: "whatsapp", AgentID: "ag1", Channel: "whatsapp"})
		svc.End(aa.EndRequest{WorkspaceID: "ws", EntryID: "e1", EntryType: "whatsapp", Outcome: aa.OutcomeHandedOff, Reason: aa.EndReasonManualAssignment, HandoffTo: "user-2", EndedBy: tc.endedBy})

		ev := endedEvent(t, fl)
		if ev.ActorKind != tc.kind || (tc.id != "" && ev.ActorID != tc.id && ev.ActorID != tc.endedBy) {
			t.Errorf("%s: actor = %s/%s, want %s", name, ev.ActorKind, ev.ActorID, tc.kind)
		}
		details := ev.DetailsMap()
		if details["agent_id"] != "ag1" || details["from_actor_id"] != "ai:ag1" || details["handoff_to"] != "user-2" || details["reason"] != aa.EndReasonManualAssignment {
			t.Errorf("%s: details = %v", name, details)
		}
	}
}

func TestTheHandOffTargetIsResolvedLikeAnyRecipient(t *testing.T) {
	if got := ce.LookupDetailID(map[string]string{"handoff_to": "user-2"}, ce.ToActorIDKeys); got != "user-2" {
		t.Fatalf("to id = %q", got)
	}
	if got := ce.LookupDetailID(map[string]string{"from_actor_id": "ai:ag1"}, ce.FromActorIDKeys); got != "ai:ag1" {
		t.Fatalf("from id = %q", got)
	}
}
