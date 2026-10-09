package geocoding_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"vozko/domain/geocoding"
	"vozko/domain/lead"
	"vozko/domain/metrics"
)

var errSweeperIncomplete = errors.New("geocoding sweeper: a required dependency is missing")

type Resolver interface {
	Resolve(ctx context.Context, claims []geocoding.Claim, deadline time.Time) BatchResult
}

type SweeperDeps struct {
	Queue    geocoding.Queue
	Chain    Resolver
	Metrics  metrics.GeocodingMetricsRecorder
	Notifier lead.ChangeNotifier
	Now      func() time.Time
	NewToken func() string
	Budget   time.Duration
}

type Sweeper struct {
	deps   SweeperDeps
	mu     sync.Mutex
	cursor string
}

type sweep struct {
	deadline time.Time
	changed  map[string]bool
	order    []string
}

func (w *sweep) touched(workspaceIDs []string) {
	for _, ws := range workspaceIDs {
		if !w.changed[ws] {
			w.changed[ws] = true
			w.order = append(w.order, ws)
		}
	}
}

func NewSweeper(deps SweeperDeps) (*Sweeper, error) {
	switch {
	case deps.Queue == nil:
		return nil, fmt.Errorf("%w: queue", errSweeperIncomplete)
	case deps.Chain == nil:
		return nil, fmt.Errorf("%w: chain", errSweeperIncomplete)
	case deps.Metrics == nil:
		return nil, fmt.Errorf("%w: metrics", errSweeperIncomplete)
	case deps.Notifier == nil:
		return nil, fmt.Errorf("%w: notifier", errSweeperIncomplete)
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	if deps.NewToken == nil {
		deps.NewToken = uuid.NewString
	}
	if deps.Budget <= 0 {
		deps.Budget = geocoding.SweepBudget
	}
	return &Sweeper{deps: deps}, nil
}

func (s *Sweeper) Sweep(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := &sweep{deadline: s.deps.Now().Add(s.deps.Budget), changed: map[string]bool{}}
	defer func() {
		if len(run.order) > 0 {
			s.deps.Queue.RefreshAggregates(run.order)
		}
	}()
	defer s.reportBacklog(context.WithoutCancel(ctx))
	if _, err := s.deps.Queue.Arm(ctx, s.deps.Now()); err != nil {
		return fmt.Errorf("geocoding sweep: arm unscheduled addresses: %w", err)
	}
	for s.deps.Now().Before(run.deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		more, err := s.batch(ctx, run)
		if err != nil {
			return err
		}
		if !more {
			break
		}
	}
	return nil
}

func (s *Sweeper) batch(ctx context.Context, run *sweep) (bool, error) {
	token := s.deps.NewToken()
	claims, err := s.deps.Queue.Claim(ctx, geocoding.ClaimRequest{
		Token: token, Now: s.deps.Now(), Lease: geocoding.Lease,
		Limit: geocoding.BatchSize, WorkspaceTurn: geocoding.WorkspaceTurn, After: s.cursor,
	})
	if err != nil {
		return false, fmt.Errorf("geocoding sweep: claim: %w", err)
	}
	if len(claims) == 0 {
		return false, nil
	}
	s.cursor = geocoding.NextTurn(s.cursor, claims)
	result := s.deps.Chain.Resolve(ctx, claims, run.deadline)
	if len(result.Settlements) > 0 {
		written, err := s.deps.Queue.Settle(ctx, token, s.deps.Now(), result.Settlements)
		if err != nil {
			return false, fmt.Errorf("geocoding sweep: write back %d addresses: %w", len(result.Settlements), err)
		}
		run.touched(written.Workspaces)
		s.deps.Metrics.AddGeocodingStale(written.Stale)
		s.countOutcomes(result.Settlements)
		s.announce(written.Changed)
	}
	if len(result.Release) > 0 {
		if err := s.deps.Queue.Release(ctx, token, result.Release); err != nil {
			log.Printf("[geocoding] releasing %d claims failed, their lease will expire: %v", len(result.Release), err)
		}
	}
	return true, nil
}

func (s *Sweeper) countOutcomes(settlements []geocoding.Settlement) {
	counts := map[string]int{}
	for _, settlement := range settlements {
		counts[settlement.Resolution.Outcome()]++
	}
	for outcome, n := range counts {
		s.deps.Metrics.AddGeocodingOutcomes(outcome, n)
	}
}

func (s *Sweeper) reportBacklog(ctx context.Context) {
	backlog, err := s.deps.Queue.Backlog(ctx)
	if err != nil {
		log.Printf("[geocoding] backlog unreadable: %v", err)
		return
	}
	for _, status := range lead.QueuedGeoStatuses() {
		s.deps.Metrics.SetGeocodingBacklog(string(status), backlog[status])
	}
}

func (s *Sweeper) announce(changes []lead.Change) {
	perWorkspace := map[string][]lead.Change{}
	for _, change := range changes {
		perWorkspace[change.WorkspaceID] = append(perWorkspace[change.WorkspaceID], change)
	}
	for _, group := range perWorkspace {
		if len(group) > geocoding.AnnounceLimit {
			continue
		}
		for _, change := range group {
			s.deps.Notifier.LeadChanged(change)
		}
	}
}
