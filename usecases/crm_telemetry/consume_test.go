package crm_telemetry_usecase

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	ap "vozko/domain/agent_presence"
	aa "vozko/domain/ai_attendance"
	ce "vozko/domain/conversation_event"
	"vozko/domain/crm_telemetry"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/messaging"
	qe "vozko/domain/queue_event"
)

type memDedupe struct {
	mu   sync.Mutex
	seen map[string]bool
}

func (d *memDedupe) Claim(id, kind string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seen == nil {
		d.seen = map[string]bool{}
	}
	if d.seen[id] {
		return false, nil
	}
	d.seen[id] = true
	return true, nil
}

func (d *memDedupe) Release(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.seen, id)
	return nil
}

type memEvents struct {
	mu  sync.Mutex
	all []*ce.ConversationEvent
}

func (m *memEvents) Create(e *ce.ConversationEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *e
	m.all = append(m.all, &cp)
	return nil
}
func (m *memEvents) ListByEntry(string, string, string, int, int) ([]*ce.ConversationEvent, int64, error) {
	return nil, 0, nil
}
func (m *memEvents) ListByEntryFiltered(string, string, string, ce.ListFilter) ([]*ce.ConversationEvent, int64, error) {
	return nil, 0, nil
}

type memHistory struct {
	closed   int
	appended []*ia.AssignmentHistory
}

func (m *memHistory) CloseOpen(string, string, string, time.Time) error {
	m.closed++
	return nil
}
func (m *memHistory) Append(h *ia.AssignmentHistory) error {
	cp := *h
	m.appended = append(m.appended, &cp)
	return nil
}
func (m *memHistory) ListByEntry(string, string, string, int, int) ([]*ia.AssignmentHistory, int64, error) {
	return nil, 0, nil
}
func (m *memHistory) GetOpen(string, string, string) (*ia.AssignmentHistory, error) {
	return nil, nil
}

type memPresence struct {
	transitions []ap.State
}

func (m *memPresence) Transition(_ string, _ string, state ap.State, _ string, _ time.Time) error {
	m.transitions = append(m.transitions, state)
	return nil
}
func (m *memPresence) Occupancy(string, *time.Time, *time.Time) ([]ap.OccupancyRow, error) {
	return nil, nil
}

type memQueue struct {
	n int
}

func (m *memQueue) Create(*qe.Event) error { m.n++; return nil }
func (m *memQueue) Stats(string, *time.Time, *time.Time) (*qe.Stats, error) {
	return &qe.Stats{}, nil
}
func (m *memQueue) StatsWithSL(string, *time.Time, *time.Time, int) (*qe.Stats, error) {
	return &qe.Stats{}, nil
}

type memAI struct {
	sessions map[string]*aa.Session
}

func newMemAI() *memAI { return &memAI{sessions: map[string]*aa.Session{}} }

func (m *memAI) Create(s *aa.Session) error {
	cp := *s
	m.sessions[s.ID] = &cp
	return nil
}
func (m *memAI) Update(s *aa.Session) error {
	cp := *s
	m.sessions[s.ID] = &cp
	return nil
}
func (m *memAI) FindOpenByEntry(ws, entry, typ string) (*aa.Session, error) {
	for _, s := range m.sessions {
		if s.WorkspaceID == ws && s.EntryID == entry && s.EntryType == typ && s.EndedAt == nil {
			cp := *s
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memAI) FindOpenByCallID(ws, callID string) (*aa.Session, error) {
	for _, s := range m.sessions {
		if s.WorkspaceID == ws && s.CallID == callID && s.EndedAt == nil {
			cp := *s
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memAI) ExistsByCallID(ws, callID string) (bool, error) {
	for _, s := range m.sessions {
		if s.CallID == callID && (ws == "" || s.WorkspaceID == ws) {
			return true, nil
		}
	}
	return false, nil
}
func (m *memAI) BackfillFromCDR(string, bool, int) (int64, error) { return 0, nil }
func (m *memAI) FindByID(id string) (*aa.Session, error) {
	s := m.sessions[id]
	if s == nil {
		return nil, aa.ErrSessionNotFound
	}
	cp := *s
	return &cp, nil
}
func (m *memAI) ListByEntry(string, string, string, int, int) ([]*aa.Session, int64, error) {
	return nil, 0, nil
}
func (m *memAI) Stats(string, *time.Time, *time.Time) (*aa.Stats, error) {
	return &aa.Stats{}, nil
}

type fakeAck struct {
	acked, nacked, requeue bool
	count                  int
}

func (a *fakeAck) Ack() error              { a.acked = true; return nil }
func (a *fakeAck) Nack(requeue bool) error { a.nacked = true; a.requeue = requeue; return nil }
func (a *fakeAck) DeliveryCount() int      { return a.count }

func TestConsumer_ConversationEvent_Idempotent(t *testing.T) {
	evRepo := &memEvents{}
	dedupe := &memDedupe{}
	c := NewConsumerWithDeps(ConsumerDeps{
		Events:    evRepo,
		History:   &memHistory{},
		AIRepo:    newMemAI(),
		QueueRepo: &memQueue{},
		Presence:  &memPresence{},
		Dedupe:    dedupe,
	}).(*consumer)

	ev := ce.New("ws", "e1", "whatsapp", ce.EventReplied).WithActorHuman("u1").Build()
	ev.ID = "evt-1"
	body, _ := json.Marshal(ev)
	env := crm_telemetry.Envelope{ID: "evt-1", Kind: crm_telemetry.KindConversationEvent, Payload: body}
	raw, _ := json.Marshal(env)

	ack1 := &fakeAck{count: 1}
	c.handle(raw, ack1)
	// allow goroutine
	time.Sleep(50 * time.Millisecond)
	if !ack1.acked || len(evRepo.all) != 1 {
		t.Fatalf("first: acked=%v n=%d", ack1.acked, len(evRepo.all))
	}

	ack2 := &fakeAck{count: 2}
	c.handle(raw, ack2)
	time.Sleep(50 * time.Millisecond)
	if !ack2.acked || len(evRepo.all) != 1 {
		t.Fatalf("second should skip: acked=%v n=%d", ack2.acked, len(evRepo.all))
	}
}

func TestConsumer_PresenceAndAssignment(t *testing.T) {
	hist := &memHistory{}
	pres := &memPresence{}
	c := NewConsumerWithDeps(ConsumerDeps{
		Events:    &memEvents{},
		History:   hist,
		AIRepo:    newMemAI(),
		QueueRepo: &memQueue{},
		Presence:  pres,
		Dedupe:    &memDedupe{},
	}).(*consumer)

	// presence
	pbody, _ := json.Marshal(crm_telemetry.PresencePayload{
		WorkspaceID: "ws", UserID: "u1", State: "online", Source: "ws_hub", At: time.Now().UTC(),
	})
	penv, _ := json.Marshal(crm_telemetry.Envelope{ID: "p1", Kind: crm_telemetry.KindPresence, Payload: pbody})
	ack := &fakeAck{count: 1}
	c.handle(penv, ack)
	time.Sleep(50 * time.Millisecond)
	if len(pres.transitions) != 1 || pres.transitions[0] != ap.StateOnline {
		t.Fatalf("presence=%v", pres.transitions)
	}

	// assignment
	abody, _ := json.Marshal(crm_telemetry.AssignmentHistoryPayload{
		ID: "ah1", WorkspaceID: "ws", EntryID: "e1", EntryType: "whatsapp",
		ActorKind: "human", AssignedActorID: "u1", Trigger: "manual", StartedAt: time.Now().UTC(),
	})
	aenv, _ := json.Marshal(crm_telemetry.Envelope{ID: "ah1", Kind: crm_telemetry.KindAssignmentHistory, Payload: abody})
	ack2 := &fakeAck{count: 1}
	c.handle(aenv, ack2)
	time.Sleep(50 * time.Millisecond)
	if hist.closed != 1 || len(hist.appended) != 1 {
		t.Fatalf("history closed=%d n=%d", hist.closed, len(hist.appended))
	}
}

// silence unused messaging import if only MessageAck used via fake
var _ messaging.MessageAck = (*fakeAck)(nil)
