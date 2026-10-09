package geocoding_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/geocoding"
	"vozko/domain/lead"
)

type fakeQueue struct {
	batches   [][]geocoding.Claim
	claimErr  error
	settleErr error
	armed     int
	requests  []geocoding.ClaimRequest
	settled   map[string][]geocoding.Settlement
	released  map[string][]string
	backlog   map[lead.GeoStatus]int64
	onClaim   func()
	refreshed [][]string
	woken     int
}

func (q *fakeQueue) RefreshAggregates(workspaceIDs []string) {
	q.refreshed = append(q.refreshed, append([]string(nil), workspaceIDs...))
}

func (q *fakeQueue) WakeUnavailable(context.Context, time.Time) (int64, error) {
	q.woken++
	return 0, nil
}

func (q *fakeQueue) Arm(context.Context, time.Time) (int64, error) {
	q.armed++
	return 0, nil
}

func (q *fakeQueue) Claim(_ context.Context, req geocoding.ClaimRequest) ([]geocoding.Claim, error) {
	q.requests = append(q.requests, req)
	if q.onClaim != nil {
		q.onClaim()
	}
	if q.claimErr != nil {
		return nil, q.claimErr
	}
	if len(q.batches) == 0 {
		return nil, nil
	}
	next := q.batches[0]
	q.batches = q.batches[1:]
	return next, nil
}

func (q *fakeQueue) Settle(_ context.Context, token string, _ time.Time, s []geocoding.Settlement) (geocoding.SettleResult, error) {
	if q.settleErr != nil {
		return geocoding.SettleResult{}, q.settleErr
	}
	if q.settled == nil {
		q.settled = map[string][]geocoding.Settlement{}
	}
	q.settled[token] = append(q.settled[token], s...)
	result := geocoding.SettleResult{Written: len(s) - 1, Stale: 1, Leads: len(s) - 1}
	seen := map[string]bool{}
	for i, settlement := range s {
		if i == 0 {
			continue
		}
		result.Changed = append(result.Changed, lead.Change{WorkspaceID: settlement.Claim.WorkspaceID, LeadID: settlement.Claim.LeadID, Version: 2, Fields: []string{lead.FieldAddresses}})
		if !seen[settlement.Claim.WorkspaceID] {
			seen[settlement.Claim.WorkspaceID] = true
			result.Workspaces = append(result.Workspaces, settlement.Claim.WorkspaceID)
		}
	}
	return result, nil
}

func (q *fakeQueue) Release(_ context.Context, token string, ids []string) error {
	if q.released == nil {
		q.released = map[string][]string{}
	}
	q.released[token] = append(q.released[token], ids...)
	return nil
}

func (q *fakeQueue) Backlog(ctx context.Context) (map[lead.GeoStatus]int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return q.backlog, nil
}

type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func newSweeper(t *testing.T, q *fakeQueue, s *chainSetup, c *clock) *Sweeper {
	t.Helper()
	tokens := 0
	chain, err := NewChain(ChainDeps{Reference: s.reference, Settings: s.settings, Usage: s.usage, Answers: s.answers, Pauses: s.pauses, Metrics: s.metrics, Now: c.now})
	if err != nil {
		t.Fatal(err)
	}
	sweeper, err := NewSweeper(SweeperDeps{
		Queue: q, Chain: chain, Metrics: s.metrics, Notifier: s.notifier, Now: c.now,
		NewToken: func() string { tokens++; return "token-" + string(rune('0'+tokens)) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return sweeper
}

func TestNewSweeperRefusesMissingDependencies(t *testing.T) {
	s := newSetup()
	chain, _ := NewChain(ChainDeps{Reference: s.reference, Settings: s.settings, Usage: s.usage, Answers: s.answers, Pauses: s.pauses, Metrics: s.metrics})
	for name, deps := range map[string]SweeperDeps{
		"queue":    {Chain: chain, Metrics: s.metrics, Notifier: s.notifier},
		"chain":    {Queue: &fakeQueue{}, Metrics: s.metrics, Notifier: s.notifier},
		"metrics":  {Queue: &fakeQueue{}, Chain: chain, Notifier: s.notifier},
		"notifier": {Queue: &fakeQueue{}, Chain: chain, Metrics: s.metrics},
	} {
		if _, err := NewSweeper(deps); err == nil {
			t.Fatalf("NewSweeper() without %s must refuse", name)
		}
	}
}

func TestSweepLoopsBatchesUntilTheQueueIsEmpty(t *testing.T) {
	s := newSetup()
	c := &clock{at: now}
	q := &fakeQueue{
		batches: [][]geocoding.Claim{
			{claimOf("a-1", "ws-1", paulista("")), claimOf("a-2", "ws-2", paulista(""))},
			{claimOf("a-3", "ws-1", paulista(""))},
		},
		backlog: map[lead.GeoStatus]int64{lead.GeoPending: 7, lead.GeoUnavailable: 2},
	}
	if err := newSweeper(t, q, s, c).Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep() err = %v", err)
	}
	if q.armed != 1 || len(q.requests) != 3 {
		t.Fatalf("armed %d times, %d claims, want 1 arm and 3 claims (the last finds nothing)", q.armed, len(q.requests))
	}
	first := q.requests[0]
	if first.Limit != geocoding.BatchSize || first.WorkspaceTurn != geocoding.WorkspaceTurn || first.Lease != geocoding.Lease || first.Token == "" {
		t.Fatalf("claim request = %+v, want the batch, turn and lease with a token", first)
	}
	if len(q.settled[q.requests[0].Token]) != 2 || len(q.settled[q.requests[1].Token]) != 1 {
		t.Fatalf("settled = %+v, want each batch written under its own claim token", q.settled)
	}
	if s.metrics.outcomes["district"] != 3 || s.metrics.stale != 2 {
		t.Fatalf("outcomes = %+v, stale = %d, want 3 bairro points and the stale writes counted", s.metrics.outcomes, s.metrics.stale)
	}
	if s.metrics.backlog["pending"] != 7 || s.metrics.backlog["unavailable"] != 2 || s.metrics.backlog["quota_exceeded"] != 0 {
		t.Fatalf("backlog = %+v, want every queued status reported, zero included", s.metrics.backlog)
	}
}

func TestSweepStopsAtTheBudget(t *testing.T) {
	s := newSetup()
	c := &clock{at: now}
	endless := func() [][]geocoding.Claim {
		var out [][]geocoding.Claim
		for i := 0; i < 100; i++ {
			out = append(out, []geocoding.Claim{claimOf("a", "ws-1", paulista(""))})
		}
		return out
	}()
	q := &fakeQueue{batches: endless, onClaim: func() { c.at = c.at.Add(20 * time.Second) }}
	if err := newSweeper(t, q, s, c).Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep() err = %v", err)
	}
	if len(q.requests) != 3 {
		t.Fatalf("claims = %d, want 3 before the 50 second budget runs out", len(q.requests))
	}
}

func TestSweepReportsAFailedWriteAndLeavesTheLease(t *testing.T) {
	s := newSetup()
	c := &clock{at: now}
	q := &fakeQueue{batches: [][]geocoding.Claim{{claimOf("a-1", "ws-1", paulista(""))}}, settleErr: errDown}
	if err := newSweeper(t, q, s, c).Sweep(context.Background()); !errors.Is(err, errDown) {
		t.Fatalf("Sweep() err = %v, want the write error", err)
	}
}

func TestSweepReportsAFailedClaim(t *testing.T) {
	s := newSetup()
	q := &fakeQueue{claimErr: errDown}
	if err := newSweeper(t, q, s, &clock{at: now}).Sweep(context.Background()); !errors.Is(err, errDown) {
		t.Fatalf("Sweep() err = %v, want the claim error", err)
	}
}

func TestSweepReportsTheBacklogEvenWhenItFails(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name  string
		queue *fakeQueue
		ctx   context.Context
		want  error
	}{
		{"a failed claim", &fakeQueue{claimErr: errDown}, context.Background(), errDown},
		{"a failed write", &fakeQueue{batches: [][]geocoding.Claim{{claimOf("a-1", "ws-1", paulista(""))}}, settleErr: errDown}, context.Background(), errDown},
		{"a cancelled sweep", &fakeQueue{}, cancelled, context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSetup()
			tt.queue.backlog = map[lead.GeoStatus]int64{lead.GeoPending: 900, lead.GeoUnavailable: 40}
			if err := newSweeper(t, tt.queue, s, &clock{at: now}).Sweep(tt.ctx); !errors.Is(err, tt.want) {
				t.Fatalf("Sweep() err = %v, want %v", err, tt.want)
			}
			if s.metrics.backlog["pending"] != 900 || s.metrics.backlog["unavailable"] != 40 || s.metrics.backlog["quota_exceeded"] != 0 {
				t.Fatalf("backlog = %+v, want it reported after a failed sweep", s.metrics.backlog)
			}
		})
	}
}

func TestSweepSettlesAStaleClaimWithABackoffInsteadOfReleasingIt(t *testing.T) {
	s := newSetup()
	stale := claimOf("a-1", "ws-1", paulista(""))
	stale.Fingerprint = "older"
	q := &fakeQueue{batches: [][]geocoding.Claim{{stale}}}
	if err := newSweeper(t, q, s, &clock{at: now}).Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	settled := q.settled[q.requests[0].Token]
	if len(q.released) != 0 || len(settled) != 1 || settled[0].Resolution.Address.GeoStatus != lead.GeoUnavailable {
		t.Fatalf("settled = %+v, released = %+v, want the stale claim parked as unavailable", settled, q.released)
	}
}

type handBack struct{ release []string }

func (h handBack) Resolve(context.Context, []geocoding.Claim, time.Time) BatchResult {
	return BatchResult{Release: h.release}
}

func TestSweepReleasesWhatTheChainHandsBackUnderItsToken(t *testing.T) {
	s := newSetup()
	q := &fakeQueue{batches: [][]geocoding.Claim{{claimOf("a-1", "ws-1", paulista(""))}}}
	sweeper, err := NewSweeper(SweeperDeps{Queue: q, Chain: handBack{release: []string{"a-1"}}, Metrics: s.metrics, Notifier: s.notifier, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := q.released[q.requests[0].Token]; len(got) != 1 || got[0] != "a-1" {
		t.Fatalf("released = %+v, want the claim handed back under its token", q.released)
	}
}

func TestSweepRotatesTheStartingWorkspaceAcrossBatchesAndSweeps(t *testing.T) {
	s := newSetup()
	q := &fakeQueue{batches: [][]geocoding.Claim{
		{claimOf("a-1", "w1", paulista("")), claimOf("a-2", "w2", paulista(""))},
		{claimOf("a-3", "w3", paulista("")), claimOf("a-4", "w1", paulista(""))},
	}}
	sweeper := newSweeper(t, q, s, &clock{at: now})
	if err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	q.batches = [][]geocoding.Claim{{claimOf("a-5", "w2", paulista(""))}}
	if err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	var after []string
	for _, req := range q.requests {
		after = append(after, req.After)
	}
	want := []string{"", "w2", "w1", "w1", "w2"}
	if len(after) != len(want) {
		t.Fatalf("cursors = %q, want %q", after, want)
	}
	for i := range want {
		if after[i] != want[i] {
			t.Fatalf("cursors = %q, want %q", after, want)
		}
	}
}

func TestSweepRefreshesTheAggregatesOfEachChangedWorkspaceOnce(t *testing.T) {
	s := newSetup()
	q := &fakeQueue{batches: [][]geocoding.Claim{
		{claimOf("a-1", "ws-1", paulista("")), claimOf("a-2", "ws-1", paulista(""))},
		{claimOf("a-3", "ws-1", paulista("")), claimOf("a-4", "ws-1", paulista(""))},
		{claimOf("a-5", "ws-2", paulista("")), claimOf("a-6", "ws-1", paulista(""))},
	}}
	if err := newSweeper(t, q, s, &clock{at: now}).Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(q.refreshed) != 1 || len(q.refreshed[0]) != 1 || q.refreshed[0][0] != "ws-1" {
		t.Fatalf("refreshed = %+v, want ws-1 once at the end of the sweep", q.refreshed)
	}
}

func TestSweepRefreshesWhatItChangedEvenWhenALaterBatchFails(t *testing.T) {
	s := newSetup()
	q := &fakeQueue{batches: [][]geocoding.Claim{{claimOf("a-1", "ws-1", paulista("")), claimOf("a-2", "ws-1", paulista(""))}}}
	q.onClaim = func() {
		if len(q.requests) == 2 {
			q.claimErr = errDown
		}
	}
	if err := newSweeper(t, q, s, &clock{at: now}).Sweep(context.Background()); !errors.Is(err, errDown) {
		t.Fatalf("Sweep() err = %v, want the claim error", err)
	}
	if len(q.refreshed) != 1 || q.refreshed[0][0] != "ws-1" {
		t.Fatalf("refreshed = %+v, want the first batch's workspace refreshed", q.refreshed)
	}
}

func TestSweepAnnouncesAFewChangedLeadsButNotABulkImport(t *testing.T) {
	s := newSetup()
	few := []geocoding.Claim{claimOf("a-0", "ws-1", paulista("")), claimOf("a-1", "ws-1", paulista("")), claimOf("a-2", "ws-1", paulista(""))}
	var many []geocoding.Claim
	many = append(many, claimOf("b-0", "ws-2", paulista("")))
	for i := 0; i < geocoding.AnnounceLimit+1; i++ {
		many = append(many, claimOf("b-"+string(rune('A'+i%26))+string(rune('a'+i/26)), "ws-2", paulista("")))
	}
	q := &fakeQueue{batches: [][]geocoding.Claim{few, many}}
	if err := newSweeper(t, q, s, &clock{at: now}).Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	per := map[string]int{}
	for _, change := range s.notifier.changes {
		per[change.WorkspaceID]++
		if change.Version != 2 || len(change.Fields) != 1 || change.Fields[0] != lead.FieldAddresses {
			t.Fatalf("announced %+v, want the new version and the addresses field", change)
		}
	}
	if per["ws-1"] != 2 || per["ws-2"] != 0 {
		t.Fatalf("announced per workspace = %+v, want the 2 changed leads of a small batch and none of a bulk one", per)
	}
}
