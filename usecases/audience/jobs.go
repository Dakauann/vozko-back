package audience_usecase

import (
	"context"
	"errors"
	"log"
	"time"

	ca "vozko/domain/audience"
)

// The periodic jobs (plan §6.2, §6.3, §11.3, §11.4, retention). Each is a
// ctxJob for infra/cron's addChannelJob: the runner holds the distributed
// lock per job name, so a tick never overlaps itself across replicas.

const (
	// staleInFlightAfter is how long a claimed row may sit before the
	// backstop assumes its replica died (plan §8).
	staleInFlightAfter = 10 * time.Minute

	backstopContainersPerTick = 200
	staleRowsPerTick          = 2000

	// rollupLookback bounds how far back a tick rebuilds when the last-run
	// marker is missing (first boot, Redis flush).
	rollupLookback   = 3 * time.Hour
	rollupLastRunKey = "comment_analysis:rollup:last_run"

	purgeBatchSize = 5000
)

// ---- flush (30s) ----

// FlushJob turns due debounce hints into container passes.
type FlushJob struct{ engine *Engine }

func NewFlushJob(engine *Engine) *FlushJob { return &FlushJob{engine: engine} }

func (j *FlushJob) Execute(ctx context.Context) error {
	e := j.engine
	hints, err := e.Scheduler.Hints(ctx)
	if err != nil {
		return err
	}
	now := e.Clock.Now()
	cyc := newCycle()
	var firstErr error
	for _, h := range hints {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !h.Due(now, e.Debounce) {
			continue
		}
		res, err := e.ProcessContainer(ctx, h.Ref, h.WorkspaceID, cyc)
		if err != nil {
			// One container's failure must not stop the others; the first is
			// returned so the runner sees the tick failed.
			log.Printf("[comment-analysis-flush] %s: %v", h.Ref.Key(), err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		logResult("flush", h.Ref, res)
	}
	j.engine.publishPendingGauge(ctx)
	return firstErr
}

// ---- backstop (5m) ----

// BackstopJob is the DB-only sweep: containers whose hint was lost, and
// rows a dead replica left in flight. It is what makes a Redis failure cost
// latency instead of data.
type BackstopJob struct{ engine *Engine }

func NewBackstopJob(engine *Engine) *BackstopJob { return &BackstopJob{engine: engine} }

func (j *BackstopJob) Execute(ctx context.Context) error {
	e := j.engine
	now := e.Clock.Now()

	// Rows stuck in flight: attempts were counted at claim, so releasing
	// them cannot loop forever.
	stale, err := e.Repo.ListStaleInFlight(ctx, now.Add(-staleInFlightAfter), staleRowsPerTick)
	if err != nil {
		return err
	}
	if len(stale) > 0 {
		for _, r := range stale {
			r.Release(ca.ReasonDispatchInterrupted, now)
		}
		if err := e.Repo.SaveMany(ctx, stale); err != nil {
			return err
		}
		log.Printf("[comment-analysis-backstop] reset %d stale in-flight rows", len(stale))
	}

	containers, err := e.Repo.ListPendingContainers(ctx, now.Add(-e.Debounce.MaxAge), backstopContainersPerTick)
	if err != nil {
		return err
	}
	cyc := newCycle()
	var firstErr error
	for _, c := range containers {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		res, err := e.ProcessContainer(ctx, c.Ref, c.WorkspaceID, cyc)
		if err != nil {
			log.Printf("[comment-analysis-backstop] %s: %v", c.Ref.Key(), err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		logResult("backstop", c.Ref, res)
	}
	e.publishPendingGauge(ctx)
	return firstErr
}

// ---- rollup (1h) ----

// RollupJob rebuilds the daily snapshots for every day touched since the
// last run (so a backfill lands on its own days) and the author projection
// for every enabled account.
type RollupJob struct {
	repo     ca.Repository
	settings ca.SettingsRepository
	authors  ca.AuthorRepository
	rollups  ca.RollupRepository
	state    lastRunStore
	clock    ca.Clock
	// roles is the §5 author pass. Optional and attached rather than
	// constructor-injected, so a deployment without a model runs the rollup
	// exactly as it did before this existed.
	roles *RoleInferenceJob
}

// SetRoleInference attaches the author pass. It runs right after the author
// projection is rebuilt, which is the only moment the corpus sizes it decides
// on are known to be current.
func (j *RollupJob) SetRoleInference(roles *RoleInferenceJob) { j.roles = roles }

// lastRunStore is the two SharedState methods the job needs.
type lastRunStore interface {
	GetString(key string) (string, error)
	SetString(key string, value string, ttl time.Duration) error
}

func NewRollupJob(repo ca.Repository, settings ca.SettingsRepository, authors ca.AuthorRepository, rollups ca.RollupRepository, state lastRunStore, clock ca.Clock) *RollupJob {
	return &RollupJob{repo: repo, settings: settings, authors: authors, rollups: rollups, state: state, clock: clock}
}

func (j *RollupJob) Execute(ctx context.Context) error {
	now := j.clock.Now()
	since := j.lastRun(now)

	days, err := j.repo.DaysAnalyzedSince(ctx, since)
	if err != nil {
		return err
	}
	for _, day := range days {
		rows, err := j.repo.AggregateRollups(ctx, day)
		if err != nil {
			return err
		}
		for _, r := range rows {
			r.ComputedAt = now
			r.Finalize()
		}
		if err := j.rollups.UpsertMany(ctx, rows); err != nil {
			return err
		}
	}

	enabled, err := j.settings.ListEnabled(ctx)
	if err != nil {
		return err
	}
	for _, s := range enabled {
		authors, err := j.repo.AggregateAuthors(ctx, s.Source, s.AccountID, since)
		if err != nil {
			return err
		}
		for _, a := range authors {
			a.UpdatedAt = now
			a.Derive()
		}
		if err := j.authors.UpsertMany(ctx, authors); err != nil {
			return err
		}
		// Best effort and capped inside: an account whose author pass fails
		// still gets its counters, which is the part the dashboard needs.
		if j.roles != nil {
			j.roles.RunAccount(ctx, s.Source, s.AccountID)
		}
	}

	if err := j.state.SetString(rollupLastRunKey, now.UTC().Format(time.RFC3339), 0); err != nil {
		log.Printf("[comment-analysis-rollup] last-run marker not saved (next tick rebuilds %s): %v", rollupLookback, err)
	}
	return nil
}

// lastRun overlaps the previous window by a minute so a row saved while the
// last tick was running is not missed.
func (j *RollupJob) lastRun(now time.Time) time.Time {
	fallback := now.Add(-rollupLookback)
	raw, err := j.state.GetString(rollupLastRunKey)
	if err != nil || raw == "" {
		return fallback
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil || t.Before(fallback.Add(-24*time.Hour)) {
		return fallback
	}
	return t.Add(-time.Minute)
}

// ---- purge (24h) ----

// PurgeJob is retention. Rows older than the window are deleted in slices.
type PurgeJob struct {
	repo      ca.Repository
	retention time.Duration
	clock     ca.Clock
}

func NewPurgeJob(repo ca.Repository, retention time.Duration, clock ca.Clock) *PurgeJob {
	return &PurgeJob{repo: repo, retention: retention, clock: clock}
}

func (j *PurgeJob) Execute(ctx context.Context) error {
	if j.retention <= 0 {
		return nil
	}
	cutoff := j.clock.Now().Add(-j.retention)
	total := int64(0)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, err := j.repo.PurgeBefore(ctx, cutoff, purgeBatchSize)
		if err != nil {
			return err
		}
		total += n
		if n < purgeBatchSize {
			break
		}
	}
	if total > 0 {
		log.Printf("[comment-analysis-purge] deleted %d rows older than %s", total, cutoff.Format(time.RFC3339))
	}
	return nil
}

// ---- helpers ----

func (e *Engine) publishPendingGauge(ctx context.Context) {
	if e.Metrics == nil {
		return
	}
	counts, err := e.Repo.CountPendingBySource(ctx)
	if err != nil {
		return
	}
	for _, source := range []ca.Source{ca.SourceInstagram} {
		e.Metrics.SetCommentPending(string(source), counts[source])
	}
}

func logResult(job string, ref ca.ContainerRef, res containerResult) {
	if res.Analyzed == 0 && res.Released == 0 && res.Skipped == 0 && res.Deferred == 0 && res.Stopped == nil {
		return
	}
	stopped := ""
	if res.Stopped != nil {
		stopped = " stopped=" + res.Stopped.Error()
	}
	log.Printf("[comment-analysis-%s] %s analyzed=%d released=%d skipped=%d deferred=%d batches=%d%s",
		job, ref.Key(), res.Analyzed, res.Released, res.Skipped, res.Deferred, res.Batches, stopped)
}

// IsCapStop reports whether an error is one of the expected early stops.
func IsCapStop(err error) bool {
	return errors.Is(err, ca.ErrDailyCapReached) || errors.Is(err, ca.ErrBalanceBelowFloor) || errors.Is(err, ca.ErrCycleCapReached)
}
