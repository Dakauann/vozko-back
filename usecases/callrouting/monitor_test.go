package callrouting_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/callrouting"
	"vozko/domain/callsession"
)

func (p presence) ListPresence(workspaceID string) []callsession.MemberPresence {
	out := make([]callsession.MemberPresence, 0, len(p.operators))
	for _, o := range p.operators {
		o.mu.Lock()
		out = append(out, callsession.MemberPresence{UserID: o.userID, OnCall: o.onCall, Ringing: o.reserved != "" && !o.onCall, Busy: o.onCall || o.reserved != "", HasBrowser: true})
		o.mu.Unlock()
	}
	return out
}

type history []callrouting.QueueOutcome

func (h history) QueueOutcomes(context.Context, string, time.Time, time.Time) ([]callrouting.QueueOutcome, error) {
	return h, nil
}

type failingHistory struct{}

func (failingHistory) QueueOutcomes(context.Context, string, time.Time, time.Time) ([]callrouting.QueueOutcome, error) {
	return nil, errors.New("db down")
}

func TestTheManagerSeesWhoWaitsAndWhatEachMemberIsDoing(t *testing.T) {
	ana := &operator{userID: "ana", onCall: true}
	bia := &operator{userID: "bia"}
	caio := &operator{userID: "caio"}
	dani := &operator{userID: "dani"}
	f := newFixtureWith(answerPermission{"dani": false}, ana, bia, caio, dani)
	f.activity.CallEnded("ws1", "caio", time.Now())
	queue := testQueue("ana", "bia", "caio", "dani", "edu")
	queue.MaxWaitSeconds = 30
	bia.mu.Lock()
	bia.onCall = true
	bia.mu.Unlock()

	held := newHeldCall("c1")
	go func() { _, _ = f.dispatcher.Enqueue(context.Background(), EnqueueInput{Queue: queue, Call: held}) }()
	defer held.Hangup()
	waitFor(t, "the caller to wait", func() bool { return f.dispatcher.Waiting("q1") == 1 })

	monitor := NewQueueMonitor(QueueMonitorDeps{
		Queues:     listedQueues{list: []*callrouting.Queue{queue}},
		Dispatcher: f.dispatcher,
		Names:      names{"ana": "Ana Souza"},
		History:    history{},
	})
	live, err := monitor.Live(context.Background(), "ws1")
	if err != nil || len(live) != 1 {
		t.Fatalf("Live = %+v, %v", live, err)
	}
	q := live[0]
	if q.Name != "Vendas" || len(q.Waiting) != 1 || q.Waiting[0].CallID != "c1" || q.Waiting[0].RemoteNumber != "5584994409684" || q.Waiting[0].Since.IsZero() {
		t.Fatalf("waiting = %+v", q.Waiting)
	}
	states := map[string]callrouting.AgentState{}
	for _, agent := range q.Agents {
		states[agent.UserID] = agent.State
	}
	want := map[string]callrouting.AgentState{
		"ana": callrouting.AgentOnCall, "bia": callrouting.AgentOnCall, "caio": callrouting.AgentWrapUp,
		"dani": callrouting.AgentOffline, "edu": callrouting.AgentOffline,
	}
	for user, state := range want {
		if states[user] != state {
			t.Errorf("%s = %s, want %s", user, states[user], state)
		}
	}
	if q.Agents[0].Name != "Ana Souza" {
		t.Fatalf("agents = %+v, want names resolved", q.Agents)
	}
}

func TestTheDailyNumbersCoverEveryQueueEvenIdleOnes(t *testing.T) {
	vendas := testQueue("ana")
	suporte := testQueue("ana")
	suporte.ID, suporte.Name = "q2", "Suporte"
	f := newFixture()
	monitor := NewQueueMonitor(QueueMonitorDeps{
		Queues:     listedQueues{list: []*callrouting.Queue{vendas, suporte}},
		Dispatcher: f.dispatcher,
		History: history{
			{QueueID: "q1", Outcome: callrouting.OutcomeConnected, Wait: 10 * time.Second},
			{QueueID: "q1", Outcome: callrouting.OutcomeAbandoned, Wait: 50 * time.Second},
			{QueueID: "deleted-queue", Outcome: callrouting.OutcomeConnected, Wait: time.Second},
		},
	})
	day := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	tallies, err := monitor.Stats(context.Background(), "ws1", day, day.Add(24*time.Hour))
	if err != nil || len(tallies) != 2 {
		t.Fatalf("Stats = %+v, %v", tallies, err)
	}
	if tallies[0].QueueID != "q1" || tallies[0].Offered != 2 || tallies[0].Answered != 1 || tallies[0].Abandoned != 1 || tallies[0].AnsweredWithinTarget != 1 {
		t.Fatalf("vendas = %+v", tallies[0])
	}
	if tallies[1].QueueID != "q2" || tallies[1].Offered != 0 {
		t.Fatalf("suporte = %+v", tallies[1])
	}
}

func TestDailyNumbersNeedAValidWindowAndReadableHistory(t *testing.T) {
	f := newFixture()
	day := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	monitor := NewQueueMonitor(QueueMonitorDeps{Queues: listedQueues{}, Dispatcher: f.dispatcher, History: history{}})
	if _, err := monitor.Stats(context.Background(), "ws1", day, day); !errors.Is(err, callrouting.ErrInvalidStatsWindow) {
		t.Fatalf("empty window err = %v", err)
	}
	if _, err := monitor.Stats(context.Background(), "ws1", day, day.Add(40*24*time.Hour)); !errors.Is(err, callrouting.ErrInvalidStatsWindow) {
		t.Fatalf("huge window err = %v", err)
	}
	broken := NewQueueMonitor(QueueMonitorDeps{Queues: listedQueues{}, Dispatcher: f.dispatcher, History: failingHistory{}})
	if _, err := broken.Stats(context.Background(), "ws1", day, day.Add(time.Hour)); err == nil {
		t.Fatal("a history failure was reported as an idle day")
	}
}
