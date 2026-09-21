package inbox_assignment_usecase

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ia "vozko/domain/inbox_assignment"
	wsc "vozko/domain/workspace_config"
)

type world struct {
	t       *testing.T
	clock   time.Time
	repo    *statefulRepo
	online  *mockEligible
	roster  *stubRoster
	seen    *stubLastSeen
	cfg     *rouletteConfig
	history *recordingHistory
	svc     *AssignmentService
	job     *RescueJob
	rescue  *stubRescueHistory
	att     *stubAttention
	status  *stubStatus
}

func newWorld(t *testing.T, members []string, mode string) *world {
	t.Helper()
	w := &world{
		t:      t,
		clock:  testNow,
		repo:   newStatefulRepo(),
		online: &mockEligible{},
		roster: &stubRoster{members: members},
		seen:   &stubLastSeen{seen: map[string]time.Time{}},
		cfg: &rouletteConfig{cfg: &wsc.WorkspaceConfig{
			WorkspaceID:                 "ws-1",
			RouletteMode:                mode,
			RouletteLastSeenWindowHours: 48,
			RouletteRescueEnabled:       true,
			RouletteRescueAfterMinutes:  15,
		}},
		history: &recordingHistory{},
		att:     &stubAttention{},
		status:  &stubStatus{},
	}

	w.svc = NewAssignmentService(w.repo, w.online, defaultResolver("ws-1", ""), w.cfg)
	w.svc.SetRoster(w.roster)
	w.svc.SetPresence(w.seen)
	w.svc.SetHistory(w.history)
	w.svc.Candidates().SetClock(func() time.Time { return w.clock })

	w.rescue = &stubRescueHistory{}
	w.job = NewRescueJob(&worldConfigReader{w}, w.rescue, w.att, w.status, w.svc)
	w.job.SetClock(func() time.Time { return w.clock })
	return w
}

type worldConfigReader struct{ w *world }

func (c *worldConfigReader) ListRoulettePolicies(context.Context) ([]wsc.RoulettePolicy, error) {
	cfg := c.w.cfg.cfg
	if !cfg.RouletteRescueActive() {
		return nil, nil
	}
	return []wsc.RoulettePolicy{{
		WorkspaceID:    "ws-1",
		RescueAfter:    cfg.EffectiveRouletteRescueAfter(),
		LastSeenWindow: cfg.EffectiveRouletteLastSeenWindow(),
	}}, nil
}

func (c *worldConfigReader) GetByWorkspaceID(ctx context.Context, id string) (*wsc.WorkspaceConfig, error) {
	return c.w.cfg.GetByWorkspaceID(ctx, id)
}

func (w *world) advance(d time.Duration) { w.clock = w.clock.Add(d) }

func (w *world) connect(userIDs ...string) {
	w.online.workspaceUsers = append([]string(nil), userIDs...)
	for _, uid := range userIDs {
		w.seen.seen[uid] = w.clock
	}
}

func (w *world) disconnect() {
	for _, uid := range w.online.workspaceUsers {
		w.seen.seen[uid] = w.clock
	}
	w.online.workspaceUsers = nil
}

func (w *world) inbound(entryID string) string {
	return w.svc.EnsureAssignment(entryID, "whatsapp", "phone-1")
}

func (w *world) ownerOf(entryID string) string {
	a := w.repo.assignments[assignmentKey("ws-1", entryID, "whatsapp")]
	if a == nil {
		return ""
	}
	return a.AssignedUserID
}

func (w *world) stall(entryID string, startedAt time.Time) {
	w.rescue.open = []*ia.AssignmentHistory{{
		WorkspaceID:     "ws-1",
		EntryID:         entryID,
		EntryType:       "whatsapp",
		AssignedActorID: w.ownerOf(entryID),
		Trigger:         ia.TriggerInboundRR,
		BusinessPhoneID: "phone-1",
		StartedAt:       startedAt,
	}}
}

func (w *world) sweep() {
	require.NoError(w.t, w.job.Execute(context.Background()))
}

func TestE2E_TeamGoesHome(t *testing.T) {
	w := newWorld(t, []string{"ana", "bob", "cid"}, wsc.RouletteModeLastSeen)
	w.connect("ana", "bob", "cid")

	assert.Equal(t, "ana", w.inbound("m-1"))
	assert.Equal(t, "bob", w.inbound("m-2"))
	assert.Equal(t, "cid", w.inbound("m-3"))

	w.disconnect()
	w.advance(2 * time.Hour)

	assert.NotEmpty(t, w.inbound("evening-1"))
	assert.NotEmpty(t, w.inbound("evening-2"))

	owners := map[string]bool{w.ownerOf("evening-1"): true, w.ownerOf("evening-2"): true}
	assert.Len(t, owners, 2, "two evening conversations must reach two different agents")

	w.advance(72 * time.Hour)
	assert.Equal(t, "", w.inbound("friday-1"),
		"nobody seen inside the window and nobody connected: the entry stays unassigned")
}

func TestE2E_RescueChainToExhaustion(t *testing.T) {
	w := newWorld(t, []string{"ana", "bob", "cid"}, wsc.RouletteModeLastSeen)
	w.seen.seen = map[string]time.Time{
		"ana": w.clock.Add(-1 * time.Hour),
		"bob": w.clock.Add(-2 * time.Hour),
		"cid": w.clock.Add(-3 * time.Hour),
	}

	require.Equal(t, "ana", w.inbound("stalled"))
	handout := w.clock

	w.advance(20 * time.Minute)
	w.stall("stalled", handout)
	w.rescue.hops = 0
	w.sweep()
	assert.Equal(t, "bob", w.ownerOf("stalled"))

	w.advance(20 * time.Minute)
	w.stall("stalled", handout)
	w.rescue.hops = 1
	w.sweep()
	assert.Equal(t, "cid", w.ownerOf("stalled"))

	w.advance(20 * time.Minute)
	w.stall("stalled", handout)
	w.rescue.hops = 2
	w.sweep()
	assert.Equal(t, "ana", w.ownerOf("stalled"))

	w.advance(20 * time.Minute)
	w.stall("stalled", handout)
	w.rescue.hops = 3
	w.sweep()
	assert.Equal(t, "", w.ownerOf("stalled"))

	triggers := make([]string, 0, len(w.history.appended))
	for _, h := range w.history.appended {
		triggers = append(triggers, h.Trigger)
	}
	assert.Equal(t,
		[]string{ia.TriggerInboundRR, ia.TriggerRescue, ia.TriggerRescue, ia.TriggerRescue},
		triggers)
	assert.Equal(t, 5, w.history.closed)
	assert.Len(t, w.history.appended, 4)
}

func TestE2E_RescueInterruptedByTheOwner(t *testing.T) {
	w := newWorld(t, []string{"ana", "bob"}, wsc.RouletteModeLastSeen)
	w.seen.seen = map[string]time.Time{
		"ana": w.clock.Add(-1 * time.Hour),
		"bob": w.clock.Add(-2 * time.Hour),
	}

	require.Equal(t, "ana", w.inbound("read-in-time"))
	handout := w.clock

	w.advance(14 * time.Minute)
	w.stall("read-in-time", handout)
	w.sweep()
	assert.Equal(t, "ana", w.ownerOf("read-in-time"))

	w.att.attended = true
	w.advance(6 * time.Minute)
	w.stall("read-in-time", handout)
	w.sweep()
	assert.Equal(t, "ana", w.ownerOf("read-in-time"))
}

func TestE2E_ModeFlipStopsPendingRescues(t *testing.T) {
	w := newWorld(t, []string{"ana", "bob"}, wsc.RouletteModeLastSeen)
	w.seen.seen = map[string]time.Time{
		"ana": w.clock.Add(-1 * time.Hour),
		"bob": w.clock.Add(-2 * time.Hour),
	}

	require.Equal(t, "ana", w.inbound("in-flight"))
	handout := w.clock

	w.cfg.cfg.RouletteMode = wsc.RouletteModeOnline

	w.advance(30 * time.Minute)
	w.stall("in-flight", handout)
	w.sweep()
	assert.Equal(t, "ana", w.ownerOf("in-flight"))

	assert.Equal(t, "", w.inbound("after-flip"), "nobody is connected in online mode")
	w.connect("cid")
	assert.Equal(t, "cid", w.inbound("after-flip-2"))
}

func TestE2E_PresenceOutageFallsBackToTheOnlinePool(t *testing.T) {
	w := newWorld(t, []string{"ana", "bob"}, wsc.RouletteModeLastSeen)
	w.seen.seen = map[string]time.Time{
		"ana": w.clock.Add(-1 * time.Hour),
		"bob": w.clock.Add(-2 * time.Hour),
	}
	require.Equal(t, "ana", w.inbound("before-outage"))

	w.advance(72 * time.Hour)
	w.online.workspaceUsers = []string{"working"}

	assert.Equal(t, "working", w.inbound("during-outage"),
		"an empty last-seen ring must fall back to the connected pool, not stop distribution")
}

func TestE2E_IdenticalAcrossChannels(t *testing.T) {
	owners := map[string][]string{}
	for _, entryType := range []string{"whatsapp", "unofficial_whatsapp", "telegram", "instagram"} {
		w := newWorld(t, []string{"ana", "bob", "cid"}, wsc.RouletteModeLastSeen)
		w.seen.seen = map[string]time.Time{
			"ana": w.clock.Add(-1 * time.Hour),
			"bob": w.clock.Add(-2 * time.Hour),
			"cid": w.clock.Add(-3 * time.Hour),
		}
		for i := 0; i < 4; i++ {
			owners[entryType] = append(owners[entryType],
				w.svc.EnsureAssignment(fmt.Sprintf("e-%d", i), entryType, "acct-1"))
		}
	}

	want := []string{"ana", "bob", "cid", "ana"}
	for entryType, got := range owners {
		assert.Equal(t, want, got, "channel %s must distribute identically", entryType)
	}
}

func TestE2E_OnlineModeIgnoresTheLastSeenReaders(t *testing.T) {
	w := newWorld(t, []string{"offline-1", "offline-2"}, wsc.RouletteModeOnline)
	w.seen.seen = map[string]time.Time{
		"offline-1": w.clock.Add(-1 * time.Hour),
		"offline-2": w.clock.Add(-2 * time.Hour),
	}

	assert.Equal(t, "", w.inbound("nobody-online"),
		"the online mode must not reach into the last-seen pool")
	assert.Empty(t, w.roster.calls, "the roster must not be consulted in the online mode")

	w.connect("zoe", "adam")
	assert.Equal(t, "adam", w.inbound("someone-online"),
		"the online ring is still sorted by user id")
	assert.Equal(t, "zoe", w.inbound("someone-online-2"))
}
