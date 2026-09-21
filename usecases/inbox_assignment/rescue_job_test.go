package inbox_assignment_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	ia "vozko/domain/inbox_assignment"
	wsc "vozko/domain/workspace_config"
)

type stubRescueConfig struct {
	policies    []wsc.RoulettePolicy
	policiesErr error
	cfg         *wsc.WorkspaceConfig
	cfgErr      error
	cfgReads    int
}

func (c *stubRescueConfig) ListRoulettePolicies(context.Context) ([]wsc.RoulettePolicy, error) {
	return c.policies, c.policiesErr
}
func (c *stubRescueConfig) GetByWorkspaceID(context.Context, string) (*wsc.WorkspaceConfig, error) {
	c.cfgReads++
	return c.cfg, c.cfgErr
}

type stubRescueHistory struct {
	open     []*ia.AssignmentHistory
	openErr  error
	hops     int
	hopsErr  error
	listArgs struct {
		workspaceIDs []string
		triggers     []string
		olderThan    time.Time
		limit        int
	}
}

func (h *stubRescueHistory) ListOpenOlderThan(workspaceIDs []string, triggers []string, olderThan time.Time, limit int) ([]*ia.AssignmentHistory, error) {
	h.listArgs.workspaceIDs = workspaceIDs
	h.listArgs.triggers = triggers
	h.listArgs.olderThan = olderThan
	h.listArgs.limit = limit
	return h.open, h.openErr
}
func (h *stubRescueHistory) CountRescuesSinceHandout(string, string, string) (int, error) {
	return h.hops, h.hopsErr
}

type stubAttention struct {
	attended bool
	err      error
	calls    int
}

func (a *stubAttention) AttendedSince(string, string, string, time.Time) (bool, error) {
	a.calls++
	return a.attended, a.err
}

type stubStatus struct {
	status conversation.ConversationStatus
}

func (s *stubStatus) GetConversationStatus(string, string) conversation.ConversationStatus {
	return s.status
}

type rescueFixture struct {
	job         *RescueJob
	repo        *statefulRepo
	cfg         *stubRescueConfig
	history     *stubRescueHistory
	att         *stubAttention
	status      *stubStatus
	svc         *AssignmentService
	svcLastSeen *stubLastSeen
}

func rescueConfigWith(mut func(*wsc.WorkspaceConfig)) *wsc.WorkspaceConfig {
	cfg := &wsc.WorkspaceConfig{
		WorkspaceID:                 "ws-1",
		RouletteMode:                wsc.RouletteModeLastSeen,
		RouletteLastSeenWindowHours: 48,
		RouletteRescueEnabled:       true,
		RouletteRescueAfterMinutes:  15,
	}
	if mut != nil {
		mut(cfg)
	}
	return cfg
}

func newRescueFixture(t *testing.T, ring []string, seen map[string]time.Time, open ...*ia.AssignmentHistory) *rescueFixture {
	t.Helper()

	repo := newStatefulRepo()
	cfg := &stubRescueConfig{
		policies: []wsc.RoulettePolicy{{WorkspaceID: "ws-1", RescueAfter: 15 * time.Minute, LastSeenWindow: 48 * time.Hour}},
		cfg:      rescueConfigWith(nil),
	}
	lastSeen := &stubLastSeen{seen: seen}
	svc := lastSeenService(repo, defaultEligible(), &stubRoster{members: ring}, lastSeen, cfg, "")

	f := &rescueFixture{
		repo:        repo,
		cfg:         cfg,
		history:     &stubRescueHistory{open: open},
		att:         &stubAttention{},
		status:      &stubStatus{status: conversation.ConversationStatusOngoing},
		svc:         svc,
		svcLastSeen: lastSeen,
	}
	f.job = NewRescueJob(f.cfg, f.history, f.att, f.status, svc)
	f.job.SetClock(func() time.Time { return testNow })
	return f
}

func openInterval(entryID, owner string, startedAt time.Time) *ia.AssignmentHistory {
	return &ia.AssignmentHistory{
		WorkspaceID:     "ws-1",
		EntryID:         entryID,
		EntryType:       "whatsapp",
		AssignedActorID: owner,
		Trigger:         ia.TriggerInboundRR,
		BusinessPhoneID: "phone-1",
		StartedAt:       startedAt,
	}
}

func minutesAgo(m int) time.Time { return testNow.Add(-time.Duration(m) * time.Minute) }

func (f *rescueFixture) ownerOf(entryID string) string {
	a := f.repo.assignments[assignmentKey("ws-1", entryID, "whatsapp")]
	if a == nil {
		return ""
	}
	return a.AssignedUserID
}

func (f *rescueFixture) seedAssignment(entryID, owner string) {
	f.repo.assignments[assignmentKey("ws-1", entryID, "whatsapp")] = &ia.InboxAssignment{
		WorkspaceID: "ws-1", EntryID: entryID, EntryType: "whatsapp", AssignedUserID: owner,
	}
}

func TestRescue_MovesAnUnattendedConversationToTheNextInTheRing(t *testing.T) {
	f := newRescueFixture(t,
		[]string{"ana", "bob", "cid"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2), "cid": hoursAgo(3)},
		openInterval("entry-1", "bob", minutesAgo(20)))
	f.seedAssignment("entry-1", "bob")

	require.NoError(t, f.job.Execute(context.Background()))
	assert.Equal(t, "cid", f.ownerOf("entry-1"))
}

func TestRescue_NeverTouchesTheRoundRobinPointer(t *testing.T) {
	f := newRescueFixture(t,
		[]string{"ana", "bob", "cid"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2), "cid": hoursAgo(3)},
		openInterval("entry-1", "bob", minutesAgo(20)))
	f.seedAssignment("entry-1", "bob")
	f.repo.rrStates[rrKey("ws-1", "phone-1", "")] = &ia.RoundRobinState{
		WorkspaceID: "ws-1", BusinessPhoneID: "phone-1", LastAssignedUserID: "ana",
	}

	require.NoError(t, f.job.Execute(context.Background()))

	assert.Equal(t, "ana", f.repo.rrStates[rrKey("ws-1", "phone-1", "")].LastAssignedUserID,
		"the pointer must be untouched, or the next inbound conversation skips an agent who did nothing wrong")
}

func TestRescue_GoesThroughTheAssignmentChokePoint(t *testing.T) {
	f := newRescueFixture(t,
		[]string{"ana", "bob"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2)},
		openInterval("entry-1", "bob", minutesAgo(20)))
	f.seedAssignment("entry-1", "bob")

	history := &recordingHistory{}
	f.svc.SetHistory(history)

	require.NoError(t, f.job.Execute(context.Background()))

	require.Len(t, history.appended, 1)
	assert.Equal(t, ia.TriggerRescue, history.appended[0].Trigger)
	assert.Equal(t, "ana", history.appended[0].AssignedActorID)
	assert.Equal(t, "bob", history.appended[0].PreviousActorID)
	assert.Equal(t, 1, history.closed, "the previous ownership interval must be closed")
}

func TestRescue_SkipReasons(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(*rescueFixture)
		reason  string
	}{
		{
			name:    "E39 the owner opened it",
			arrange: func(f *rescueFixture) { f.att.attended = true },
			reason:  "an attended conversation must stay with its owner",
		},
		{
			name:    "E43 the conversation is finished",
			arrange: func(f *rescueFixture) { f.status.status = conversation.ConversationStatusFinished },
			reason:  "a finished conversation has nobody waiting",
		},
		{
			name:    "the deadline has not passed",
			arrange: func(f *rescueFixture) { f.history.open[0].StartedAt = minutesAgo(5) },
			reason:  "15 minutes have not elapsed",
		},
		{
			name:    "E54 the owner is an AI",
			arrange: func(f *rescueFixture) { f.history.open[0].AssignedActorID = "ai:agent-1" },
			reason:  "AI ownership has its own hand-off path",
		},
		{
			name: "E9 the workspace left last_seen mode",
			arrange: func(f *rescueFixture) {
				f.cfg.cfg = rescueConfigWith(func(c *wsc.WorkspaceConfig) { c.RouletteMode = wsc.RouletteModeOnline })
			},
			reason: "a mode switch must stop pending rescues",
		},
		{
			name: "rescue was disabled",
			arrange: func(f *rescueFixture) {
				f.cfg.cfg = rescueConfigWith(func(c *wsc.WorkspaceConfig) { c.RouletteRescueEnabled = false })
			},
			reason: "the toggle must be honoured",
		},
		{
			name:    "the attention check failed",
			arrange: func(f *rescueFixture) { f.att.err = errors.New("db down") },
			reason:  "a failed read must not move a conversation on a guess",
		},
		{
			name:    "the hop count failed",
			arrange: func(f *rescueFixture) { f.history.hopsErr = errors.New("db down") },
			reason:  "a failed read must not move a conversation on a guess",
		},
		{
			name:    "the config read failed",
			arrange: func(f *rescueFixture) { f.cfg.cfgErr = errors.New("db down") },
			reason:  "a failed read must not move a conversation on a guess",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRescueFixture(t,
				[]string{"ana", "bob", "cid"},
				map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2), "cid": hoursAgo(3)},
				openInterval("entry-1", "bob", minutesAgo(20)))
			f.seedAssignment("entry-1", "bob")
			tc.arrange(f)

			require.NoError(t, f.job.Execute(context.Background()))
			assert.Equal(t, "bob", f.ownerOf("entry-1"), tc.reason)
		})
	}
}

func TestRescue_RingOfOneIsLeftAlone(t *testing.T) {
	f := newRescueFixture(t,
		[]string{"bob"},
		map[string]time.Time{"bob": hoursAgo(2)},
		openInterval("entry-1", "bob", minutesAgo(20)))
	f.seedAssignment("entry-1", "bob")

	require.NoError(t, f.job.Execute(context.Background()))
	assert.Equal(t, "bob", f.ownerOf("entry-1"))
}

func TestRescue_EmptyRingLeavesTheOwner(t *testing.T) {
	f := newRescueFixture(t, nil, nil, openInterval("entry-1", "bob", minutesAgo(20)))
	f.seedAssignment("entry-1", "bob")

	require.NoError(t, f.job.Execute(context.Background()))
	assert.Equal(t, "bob", f.ownerOf("entry-1"))
}

func TestRescue_ExhaustionUnassigns(t *testing.T) {
	f := newRescueFixture(t,
		[]string{"ana", "bob", "cid"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2), "cid": hoursAgo(3)},
		openInterval("entry-1", "bob", minutesAgo(20)))
	f.seedAssignment("entry-1", "bob")
	f.history.hops = 3

	events := &recordingEvents{}
	f.svc.SetEventLogger(events)

	require.NoError(t, f.job.Execute(context.Background()))

	assert.Equal(t, "", f.ownerOf("entry-1"), "an exhausted conversation must become unassigned")
	require.NotEmpty(t, events.logged)
	last := events.logged[len(events.logged)-1]
	assert.Equal(t, ce.EventUnassigned, last.EventType)

	details := map[string]string{}
	require.NoError(t, json.Unmarshal([]byte(last.Details), &details))
	assert.Equal(t, TriggerRescueExhausted, details["trigger"])
	assert.Equal(t, "bob", details["from_user_id"])
	assert.NotContains(t, details, "to_user_id", "an unassignment has no new owner to name")
}

func TestRescue_HopCapIsBoundedByMaxRescueHops(t *testing.T) {
	ring := make([]string, 0, 10)
	seen := map[string]time.Time{}
	for i := 0; i < 10; i++ {
		uid := fmt.Sprintf("u-%d", i)
		ring = append(ring, uid)
		seen[uid] = hoursAgo(float64(i + 1))
	}

	f := newRescueFixture(t, ring, seen, openInterval("entry-1", "u-0", minutesAgo(20)))
	f.seedAssignment("entry-1", "u-0")
	f.history.hops = MaxRescueHops

	require.NoError(t, f.job.Execute(context.Background()))
	assert.Equal(t, "", f.ownerOf("entry-1"), "MaxRescueHops must cap a ring larger than it")
}

func TestRescue_NoEligibleWorkspacesDoesNothing(t *testing.T) {
	f := newRescueFixture(t, []string{"ana"}, map[string]time.Time{"ana": hoursAgo(1)})
	f.cfg.policies = nil

	require.NoError(t, f.job.Execute(context.Background()))
	assert.Nil(t, f.history.listArgs.workspaceIDs, "the candidate query must not run when nothing is eligible")
}

func TestRescue_QueriesHandoutsAndItsOwnHops(t *testing.T) {
	f := newRescueFixture(t,
		[]string{"ana", "bob"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2)},
		openInterval("entry-1", "bob", minutesAgo(20)))
	f.seedAssignment("entry-1", "bob")

	require.NoError(t, f.job.Execute(context.Background()))

	assert.Equal(t, []string{"ws-1"}, f.history.listArgs.workspaceIDs)
	assert.Equal(t, ia.RescueCandidateTriggers, f.history.listArgs.triggers)
	assert.ElementsMatch(t, []string{ia.TriggerInboundRR, ia.TriggerRescue}, f.history.listArgs.triggers,
		"a hand-out is a candidate, and so is a conversation a previous hop moved")
	assert.NotContains(t, f.history.listArgs.triggers, ia.TriggerManual,
		"a human took responsibility; the sweep must not take it back off them")
	assert.NotContains(t, f.history.listArgs.triggers, ia.TriggerOpen,
		"an agent opening a conversation ends the chain")
	assert.Equal(t, testNow.Add(-15*time.Minute), f.history.listArgs.olderThan)
	assert.Equal(t, DefaultRescueBatch, f.history.listArgs.limit)
}

func TestRescue_PerWorkspaceDeadlines(t *testing.T) {
	f := newRescueFixture(t,
		[]string{"ana", "bob"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2)},
		openInterval("entry-slow", "bob", minutesAgo(20)))
	f.seedAssignment("entry-slow", "bob")
	f.cfg.policies = []wsc.RoulettePolicy{
		{WorkspaceID: "ws-fast", RescueAfter: 5 * time.Minute},
		{WorkspaceID: "ws-1", RescueAfter: 60 * time.Minute},
	}

	require.NoError(t, f.job.Execute(context.Background()))

	assert.Equal(t, testNow.Add(-5*time.Minute), f.history.listArgs.olderThan,
		"the query is bounded by the shortest deadline among eligible workspaces")
	assert.Equal(t, "bob", f.ownerOf("entry-slow"),
		"a workspace with a 60-minute deadline must not be rescued at 20 minutes")
}

func TestRescue_ReadErrorsAreNotFatal(t *testing.T) {
	f := newRescueFixture(t, []string{"ana"}, map[string]time.Time{"ana": hoursAgo(1)})
	f.cfg.policiesErr = errors.New("db down")
	require.NoError(t, f.job.Execute(context.Background()))

	f2 := newRescueFixture(t, []string{"ana"}, map[string]time.Time{"ana": hoursAgo(1)})
	f2.history.openErr = errors.New("db down")
	require.NoError(t, f2.job.Execute(context.Background()))
}

func TestRescue_ContinuesPastASkippedEntry(t *testing.T) {
	f := newRescueFixture(t,
		[]string{"ana", "bob", "cid"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2), "cid": hoursAgo(3)},
		openInterval("entry-fresh", "bob", minutesAgo(2)),
		openInterval("entry-stale", "bob", minutesAgo(30)),
	)
	f.seedAssignment("entry-fresh", "bob")
	f.seedAssignment("entry-stale", "bob")

	require.NoError(t, f.job.Execute(context.Background()))

	assert.Equal(t, "bob", f.ownerOf("entry-fresh"))
	assert.Equal(t, "cid", f.ownerOf("entry-stale"))
}

func TestRescue_DisabledByEnv(t *testing.T) {
	t.Setenv("ASSIGNMENT_RESCUE_DISABLED", "1")

	f := newRescueFixture(t,
		[]string{"ana", "bob"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2)},
		openInterval("entry-1", "bob", minutesAgo(20)))
	f.seedAssignment("entry-1", "bob")
	f.job = NewRescueJob(f.cfg, f.history, f.att, f.status, f.svc)
	f.job.SetClock(func() time.Time { return testNow })

	require.NoError(t, f.job.Execute(context.Background()))
	assert.Equal(t, "bob", f.ownerOf("entry-1"))
	assert.Equal(t, 0, f.att.calls, "a disabled sweep must not even look")
}

type recordingHistory struct {
	appended []*ia.AssignmentHistory
	closed   int
}

func (h *recordingHistory) CloseOpen(string, string, string, time.Time) error {
	h.closed++
	return nil
}
func (h *recordingHistory) Append(a *ia.AssignmentHistory) error {
	cp := *a
	h.appended = append(h.appended, &cp)
	return nil
}
func (h *recordingHistory) ListByEntry(string, string, string, int, int) ([]*ia.AssignmentHistory, int64, error) {
	return nil, 0, nil
}
func (h *recordingHistory) GetOpen(string, string, string) (*ia.AssignmentHistory, error) {
	return nil, nil
}
func (h *recordingHistory) ListOpenOlderThan([]string, []string, time.Time, int) ([]*ia.AssignmentHistory, error) {
	return nil, nil
}
func (h *recordingHistory) CountRescuesSinceHandout(string, string, string) (int, error) {
	return 0, nil
}

type recordingEvents struct {
	logged []*ce.ConversationEvent
}

func (e *recordingEvents) Log(event *ce.ConversationEvent) { e.logged = append(e.logged, event) }

func TestRescue_ResolvesWorkspaceStateOncePerTick(t *testing.T) {
	const stalled = 50

	open := make([]*ia.AssignmentHistory, 0, stalled)
	for i := 0; i < stalled; i++ {
		open = append(open, openInterval(fmt.Sprintf("entry-%d", i), "bob", minutesAgo(20)))
	}

	f := newRescueFixture(t,
		[]string{"ana", "bob", "cid"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2), "cid": hoursAgo(3)},
		open...)
	for i := 0; i < stalled; i++ {
		f.seedAssignment(fmt.Sprintf("entry-%d", i), "bob")
	}

	seen := f.svcLastSeen
	require.NoError(t, f.job.Execute(context.Background()))

	for i := 0; i < stalled; i++ {
		assert.Equal(t, "cid", f.ownerOf(fmt.Sprintf("entry-%d", i)))
	}

	assert.Equal(t, 1, f.cfg.cfgReads,
		"one workspace must cost one config read per tick, not one per conversation")
	assert.Equal(t, 1, seen.calls,
		"one (workspace, department) ring must cost one presence query per tick")
}

func TestRescue_MemoIsScopedPerWorkspaceAndDepartment(t *testing.T) {
	f := newRescueFixture(t,
		[]string{"ana", "bob", "cid"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2), "cid": hoursAgo(3)},
		openInterval("entry-a", "bob", minutesAgo(20)),
		openInterval("entry-b", "bob", minutesAgo(20)),
	)
	f.history.open[0].DepartmentID = "dept-a"
	f.history.open[1].DepartmentID = "dept-b"
	f.seedAssignment("entry-a", "bob")
	f.seedAssignment("entry-b", "bob")

	require.NoError(t, f.job.Execute(context.Background()))

	assert.Equal(t, 1, f.cfg.cfgReads, "both entries share one workspace")
	assert.Equal(t, 2, f.svcLastSeen.calls, "two departments are two different rings")
}
