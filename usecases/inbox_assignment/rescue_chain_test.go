package inbox_assignment_usecase

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vozko/domain/actor"
	"vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
)

type intervalStore struct {
	rows []*ia.AssignmentHistory
	now  func() time.Time
}

func newIntervalStore(now func() time.Time) *intervalStore {
	return &intervalStore{now: now}
}

var _ ia.HistoryRepository = (*intervalStore)(nil)

func (s *intervalStore) Append(h *ia.AssignmentHistory) error {
	cp := *h
	cp.StartedAt = s.now()
	cp.EndedAt = nil
	s.rows = append(s.rows, &cp)
	return nil
}

func (s *intervalStore) CloseOpen(workspaceID, entryID, entryType string, _ time.Time) error {
	at := s.now()
	for _, r := range s.rows {
		if r.WorkspaceID == workspaceID && r.EntryID == entryID && r.EntryType == entryType && r.EndedAt == nil {
			ended := at
			r.EndedAt = &ended
		}
	}
	return nil
}

func (s *intervalStore) ListOpenOlderThan(workspaceIDs []string, triggers []string, olderThan time.Time, limit int) ([]*ia.AssignmentHistory, error) {
	if len(workspaceIDs) == 0 || len(triggers) == 0 {
		return nil, nil
	}
	inWorkspace := map[string]bool{}
	for _, w := range workspaceIDs {
		inWorkspace[w] = true
	}
	inTrigger := map[string]bool{}
	for _, tr := range triggers {
		inTrigger[tr] = true
	}

	var out []*ia.AssignmentHistory
	for _, r := range s.rows {
		if inWorkspace[r.WorkspaceID] && r.EndedAt == nil && inTrigger[r.Trigger] && r.StartedAt.Before(olderThan) {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *intervalStore) CountRescuesSinceHandout(workspaceID, entryID, entryType string) (int, error) {
	var rows []*ia.AssignmentHistory
	for _, r := range s.rows {
		if r.WorkspaceID == workspaceID && r.EntryID == entryID && r.EntryType == entryType {
			rows = append(rows, r)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].StartedAt.After(rows[j].StartedAt) })

	count := 0
	for _, r := range rows {
		switch r.Trigger {
		case ia.TriggerInboundRR:
			return count, nil
		case ia.TriggerRescue:
			count++
		}
	}
	return 0, nil
}

func (s *intervalStore) ListByEntry(workspaceID, entryID, entryType string, _, _ int) ([]*ia.AssignmentHistory, int64, error) {
	var out []*ia.AssignmentHistory
	for _, r := range s.rows {
		if r.WorkspaceID == workspaceID && r.EntryID == entryID && r.EntryType == entryType {
			out = append(out, r)
		}
	}
	return out, int64(len(out)), nil
}

func (s *intervalStore) GetOpen(workspaceID, entryID, entryType string) (*ia.AssignmentHistory, error) {
	for _, r := range s.rows {
		if r.WorkspaceID == workspaceID && r.EntryID == entryID && r.EntryType == entryType && r.EndedAt == nil {
			return r, nil
		}
	}
	return nil, nil
}

func (s *intervalStore) triggers(entryID string) []string {
	rows, _, _ := s.ListByEntry("ws-1", entryID, "whatsapp", 0, 0)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].StartedAt.Before(rows[j].StartedAt) })
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Trigger)
	}
	return out
}

type chainFixture struct {
	t     *testing.T
	base  *rescueFixture
	store *intervalStore
	job   *RescueJob
	now   time.Time
}

func newChainFixture(t *testing.T, ring []string, seen map[string]time.Time) *chainFixture {
	t.Helper()
	base := newRescueFixture(t, ring, seen)

	c := &chainFixture{t: t, base: base, now: testNow}
	c.store = newIntervalStore(func() time.Time { return c.now })
	base.svc.SetHistory(c.store)

	c.job = NewRescueJob(base.cfg, c.store, base.att, base.status, base.svc)
	c.job.SetClock(func() time.Time { return c.now })
	return c
}

func (c *chainFixture) handOut(entryID, owner string) {
	c.base.seedAssignment(entryID, owner)
	require.NoError(c.t, c.store.Append(&ia.AssignmentHistory{
		WorkspaceID:     "ws-1",
		EntryID:         entryID,
		EntryType:       "whatsapp",
		AssignedActorID: owner,
		Trigger:         ia.TriggerInboundRR,
		BusinessPhoneID: "phone-1",
	}))
}

func (c *chainFixture) tick() {
	c.t.Helper()
	c.now = c.now.Add(20 * time.Minute)
	require.NoError(c.t, c.job.Execute(context.Background()))
}

func (c *chainFixture) ownerOf(entryID string) string { return c.base.ownerOf(entryID) }

func TestRescueChain_WalksTheRingThenUnassigns(t *testing.T) {
	c := newChainFixture(t,
		[]string{"ana", "bob", "cid"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2), "cid": hoursAgo(3)})
	c.handOut("entry-1", "ana")

	c.tick()
	assert.Equal(t, "bob", c.ownerOf("entry-1"), "hop 1")

	c.tick()
	assert.Equal(t, "cid", c.ownerOf("entry-1"), "hop 2 — the hop that never used to happen")

	c.tick()
	assert.Equal(t, "ana", c.ownerOf("entry-1"), "hop 3 completes the lap")

	c.tick()
	assert.Equal(t, "", c.ownerOf("entry-1"),
		"ring walked: unassigned so the whole department can pick it up")

	assert.Equal(t,
		[]string{ia.TriggerInboundRR, ia.TriggerRescue, ia.TriggerRescue, ia.TriggerRescue},
		c.store.triggers("entry-1"),
		"every hand-over went through the choke point and left an interval")
}

func TestRescueChain_HopCapFollowsTheSmallerRing(t *testing.T) {
	c := newChainFixture(t,
		[]string{"ana", "bob"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2)})
	c.handOut("entry-1", "ana")

	c.tick()
	assert.Equal(t, "bob", c.ownerOf("entry-1"))
	c.tick()
	assert.Equal(t, "ana", c.ownerOf("entry-1"))
	c.tick()
	assert.Equal(t, "", c.ownerOf("entry-1"), "exhausted after two hops, not five")
}

func TestRescueChain_HopCountClimbsWithEachHop(t *testing.T) {
	c := newChainFixture(t,
		[]string{"ana", "bob", "cid"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2), "cid": hoursAgo(3)})
	c.handOut("entry-1", "ana")

	hops, err := c.store.CountRescuesSinceHandout("ws-1", "entry-1", "whatsapp")
	require.NoError(t, err)
	assert.Equal(t, 0, hops, "a fresh hand-out has moved nobody")

	for want := 1; want <= 3; want++ {
		c.tick()
		hops, err = c.store.CountRescuesSinceHandout("ws-1", "entry-1", "whatsapp")
		require.NoError(t, err)
		assert.Equal(t, want, hops)
	}
}

func TestRescueChain_StopsWhenAHopRecipientResponds(t *testing.T) {
	c := newChainFixture(t,
		[]string{"ana", "bob", "cid"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2), "cid": hoursAgo(3)})
	c.handOut("entry-1", "ana")

	c.tick()
	require.Equal(t, "bob", c.ownerOf("entry-1"))

	c.base.att.attended = true

	c.tick()
	c.tick()
	assert.Equal(t, "bob", c.ownerOf("entry-1"), "an answered conversation stays put")
	assert.Equal(t, []string{ia.TriggerInboundRR, ia.TriggerRescue}, c.store.triggers("entry-1"))
}

func TestRescueChain_ManualReassignmentEndsTheChain(t *testing.T) {
	c := newChainFixture(t,
		[]string{"ana", "bob", "cid"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2), "cid": hoursAgo(3)})
	c.handOut("entry-1", "ana")

	c.tick()
	require.Equal(t, "bob", c.ownerOf("entry-1"))

	require.NoError(t, c.base.svc.AssignManual(
		"entry-1", "whatsapp", "phone-1", "ws-1", "cid", "supervisor-1", ia.TriggerManual))

	c.tick()
	c.tick()
	c.tick()
	assert.Equal(t, "cid", c.ownerOf("entry-1"),
		"the sweep must not take a conversation back off the human who claimed it")
}

func TestRescueChain_StopsWhenTheConversationIsFinished(t *testing.T) {
	c := newChainFixture(t,
		[]string{"ana", "bob", "cid"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2), "cid": hoursAgo(3)})
	c.handOut("entry-1", "ana")

	c.tick()
	require.Equal(t, "bob", c.ownerOf("entry-1"))

	c.base.status.status = conversation.ConversationStatusFinished

	c.tick()
	c.tick()
	assert.Equal(t, "bob", c.ownerOf("entry-1"))
}

func TestRescueChain_AFreshHandoutResetsTheCount(t *testing.T) {
	c := newChainFixture(t,
		[]string{"ana", "bob"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2)})
	c.handOut("entry-1", "ana")

	c.tick()
	c.tick()
	c.tick()
	require.Equal(t, "", c.ownerOf("entry-1"), "exhausted")

	hops, err := c.store.CountRescuesSinceHandout("ws-1", "entry-1", "whatsapp")
	require.NoError(t, err)
	assert.Equal(t, 2, hops, "two hops still visible in the trail")

	c.now = c.now.Add(time.Minute)
	owner := c.base.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1")
	require.NotEmpty(t, owner)

	hops, err = c.store.CountRescuesSinceHandout("ws-1", "entry-1", "whatsapp")
	require.NoError(t, err)
	assert.Equal(t, 0, hops, "the new hand-out opens a new chain")
}

func TestRescueChain_HopsAreAttributedToTheSystem(t *testing.T) {
	c := newChainFixture(t,
		[]string{"ana", "bob"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2)})
	c.handOut("entry-1", "ana")

	c.tick()

	rows, _, _ := c.store.ListByEntry("ws-1", "entry-1", "whatsapp", 0, 0)
	var hop *ia.AssignmentHistory
	for _, r := range rows {
		if r.Trigger == ia.TriggerRescue {
			hop = r
		}
	}
	require.NotNil(t, hop)
	assert.Equal(t, actor.SystemID, hop.AssignedByActorID)
	assert.Equal(t, "ana", hop.PreviousActorID)
	assert.Equal(t, "bob", hop.AssignedActorID)
}
