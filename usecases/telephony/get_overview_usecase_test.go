package telephony_usecase

import (
	"testing"
	"time"

	"vozko/domain/agent_presence"
	"vozko/domain/dialer"
	"vozko/domain/queue_event"
	"vozko/domain/telephony"
)

type stubRepo struct {
	out *telephony.Overview
}

func (s *stubRepo) GetOverview(string, telephony.OverviewFilter) (*telephony.Overview, error) {
	return s.out, nil
}

type stubQueue struct {
	st *queue_event.Stats
}

func (s *stubQueue) Create(*queue_event.Event) error { return nil }
func (s *stubQueue) Stats(string, *time.Time, *time.Time) (*queue_event.Stats, error) {
	return s.st, nil
}
func (s *stubQueue) StatsWithSL(string, *time.Time, *time.Time, int) (*queue_event.Stats, error) {
	return s.st, nil
}

type stubPresence struct {
	rows []agent_presence.OccupancyRow
}

func (s *stubPresence) Transition(string, string, agent_presence.State, string, time.Time) error {
	return nil
}
func (s *stubPresence) Occupancy(string, *time.Time, *time.Time) ([]agent_presence.OccupancyRow, error) {
	return s.rows, nil
}

type stubLive struct {
	rows []dialer.MemberPresence
}

func (s *stubLive) Register(dialer.DialerSession) (func(), error) { return func() {}, nil }
func (s *stubLive) FindByUser(string, string) (dialer.DialerSession, bool) {
	return nil, false
}
func (s *stubLive) FindSessionsByUser(string, string) []dialer.DialerSession { return nil }
func (s *stubLive) FindByID(string) (dialer.DialerSession, bool)             { return nil, false }
func (s *stubLive) ListAvailable(string) []dialer.DialerSession              { return nil }
func (s *stubLive) ListAll(string) []dialer.DialerSession                    { return nil }
func (s *stubLive) ListPresence(string) []dialer.MemberPresence              { return s.rows }
func (s *stubLive) ListBrowserSessions(string) []dialer.DialerSession        { return nil }
func (s *stubLive) SetPresenceListener(dialer.PresenceListener)              {}
func (s *stubLive) NotifyPresenceChanged(string)                             {}

func TestGetOverview_ComposesPorts(t *testing.T) {
	uc := NewGetOverviewUseCaseWithDeps(
		&stubRepo{out: &telephony.Overview{
			KPIs:        telephony.OverviewKPIs{TotalCalls: 5, Answered: 3},
			Definitions: telephony.DefaultDefinitions(),
		}},
		&stubQueue{st: &queue_event.Stats{Enqueued: 4, Connected: 3, Abandoned: 1, AbandonRate: 25, AvgASAMs: 30000}},
		&stubPresence{rows: []agent_presence.OccupancyRow{
			{UserID: "u1", OnlineMS: 1000, OnCallMS: 500, Occupancy: 50},
		}},
		&stubLive{rows: []dialer.MemberPresence{
			{UserID: "u1", Busy: true, HasBrowser: true},
			{UserID: "u2", Busy: false, HasBranch: true},
		}},
	)
	got, err := uc.Execute("ws", telephony.OverviewFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if got.KPIs.TotalCalls != 5 {
		t.Fatalf("kpis: %+v", got.KPIs)
	}
	if !got.Queue.Available || got.Queue.Enqueued != 4 {
		t.Fatalf("queue: %+v", got.Queue)
	}
	if got.Queue.AvgASAMins == nil || *got.Queue.AvgASAMins != 0.5 {
		t.Fatalf("asa: %+v", got.Queue.AvgASAMins)
	}
	if !got.Occupancy.Available || got.Occupancy.TeamIdlePct == nil || *got.Occupancy.TeamIdlePct != 50 {
		t.Fatalf("occupancy: %+v", got.Occupancy)
	}
	if !got.Live.HasData || got.Live.Online != 2 || got.Live.InCall != 1 || got.Live.Free != 1 {
		t.Fatalf("live: %+v", got.Live)
	}
}

func TestDefaultDefinitions(t *testing.T) {
	d := telephony.DefaultDefinitions()
	if d.ConnectRate == "" || d.Gaps == "" {
		t.Fatalf("%+v", d)
	}
}
