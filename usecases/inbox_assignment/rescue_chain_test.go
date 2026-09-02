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

// intervalStore is an in-memory assignment_history that answers the sweep's
// reads with the SAME semantics as infra/repositories/assignment_history.
//
// It exists because the defect these tests guard against lived BETWEEN two
// layers and was invisible from either. The candidate query selected open
// intervals with trigger = 'inbound_rr'; the hand-over it performed closed that
// interval and opened one with trigger = 'rescue'. Each layer was correct on its
// own, and the conversation stopped moving after exactly one hop.
//
// A stub that returns a hand-set candidate list cannot express that
// interaction, which is precisely why the original suite asserted a chain the
// query layer could not produce. Anything that pretends to be this table has to
// close and open intervals for real, or it proves nothing about the chain.
type intervalStore struct {
	rows []*ia.AssignmentHistory
	now  func() time.Time
}

func newIntervalStore(now func() time.Time) *intervalStore {
	return &intervalStore{now: now}
}

var _ ia.HistoryRepository = (*intervalStore)(nil)

// Append stamps StartedAt from the test clock.
//
// AssignmentService stamps it from time.Now, which is right in production,
// where the sweep's clock is time.Now as well. Under a test clock the two would
// disagree by however far the fixture date sits from the real one and every hop
// would look like it happened in the future. Restamping here keeps the chain
// deterministic without threading a clock seam through the assignment hot path
// for the benefit of tests alone.
func (s *intervalStore) Append(h *ia.AssignmentHistory) error {
	cp := *h
	cp.StartedAt = s.now()
	cp.EndedAt = nil
	s.rows = append(s.rows, &cp)
	return nil
}

// CloseOpen mirrors: UPDATE ... SET ended_at = ?
// WHERE workspace_id = ? AND entry_id = ? AND entry_type = ? AND ended_at IS NULL
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

// ListOpenOlderThan mirrors: WHERE workspace_id IN ? AND ended_at IS NULL
// AND trigger IN ? AND started_at < ? ORDER BY started_at ASC LIMIT ?
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

// CountRescuesSinceHandout mirrors the repository: read the entry's intervals
// newest-first, count rescues, and stop at the hand-out that opened the chain.
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

// triggers returns the trigger of every interval for an entry, oldest first —
// the ownership trail a support engineer would read to explain what happened.
func (s *intervalStore) triggers(entryID string) []string {
	rows, _, _ := s.ListByEntry("ws-1", entryID, "whatsapp", 0, 0)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].StartedAt.Before(rows[j].StartedAt) })
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Trigger)
	}
	return out
}

// chainFixture drives real sweeps against a real interval store. It reuses the
// service wiring from newRescueFixture and swaps in the store, so there is one
// definition of how the assignment service is assembled for these tests.
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

// handOut is the roulette giving a conversation to its first owner.
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

// tick advances past the rescue deadline and runs one sweep.
func (c *chainFixture) tick() {
	c.t.Helper()
	c.now = c.now.Add(20 * time.Minute)
	require.NoError(c.t, c.job.Execute(context.Background()))
}

func (c *chainFixture) ownerOf(entryID string) string { return c.base.ownerOf(entryID) }

// ── the defect ──────────────────────────────────────────────────────────────

// The regression test. Before the fix this stopped at "bob" and stayed there
// forever: the sweep asked only for inbound_rr intervals, and hop 1 had already
// rewritten the open interval to rescue.
//
// Nothing here is hand-fed. Every hop is chosen by the sweep from what the
// previous hop actually wrote.
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

// The hop cap is min(len(ring), MaxRescueHops). A ring of two is exhausted
// after two hops, not after five.
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

// The counter is derived from the interval chain rather than stored, so it has
// to climb as the conversation moves. Before the fix it was always 0, which is
// what made MaxRescueHops unreachable.
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

// ── the chain must still stop for the right reasons ─────────────────────────

// Attention ends the chain wherever it arrives, not only at the first owner.
func TestRescueChain_StopsWhenAHopRecipientResponds(t *testing.T) {
	c := newChainFixture(t,
		[]string{"ana", "bob", "cid"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2), "cid": hoursAgo(3)})
	c.handOut("entry-1", "ana")

	c.tick()
	require.Equal(t, "bob", c.ownerOf("entry-1"))

	// Bob opens it and answers the customer.
	c.base.att.attended = true

	c.tick()
	c.tick()
	assert.Equal(t, "bob", c.ownerOf("entry-1"), "an answered conversation stays put")
	assert.Equal(t, []string{ia.TriggerInboundRR, ia.TriggerRescue}, c.store.triggers("entry-1"))
}

// A human taking the conversation ends the chain: the open interval is no
// longer one of RescueCandidateTriggers, so no later sweep considers it. This
// is how a supervisor stops a rescue by hand.
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

// A finished conversation is left alone even mid-chain.
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

// ── the counter resets on its own ───────────────────────────────────────────

// After exhaustion the conversation is unassigned. A new inbound message hands
// it out again, and because the hop count is read backwards from the newest
// interval and stops at the hand-out, the new chain starts at zero with no
// counter to reset and no state to keep consistent.
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

	// A new inbound message on the same conversation: a real roulette hand-out.
	c.now = c.now.Add(time.Minute)
	owner := c.base.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1")
	require.NotEmpty(t, owner)

	hops, err = c.store.CountRescuesSinceHandout("ws-1", "entry-1", "whatsapp")
	require.NoError(t, err)
	assert.Equal(t, 0, hops, "the new hand-out opens a new chain")
}

// The sweep records the system as the actor on every hop, never the agent it
// took the conversation from.
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
