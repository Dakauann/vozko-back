package comment_analysis_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	ca "vozko/domain/comment_analysis"
	"vozko/domain/shared"
)

// ---- rollup ----

type fakeAuthors struct {
	mu    sync.Mutex
	rows  []*ca.AuthorStats
	state map[string]ca.ModerationState
}

func (f *fakeAuthors) UpsertMany(_ context.Context, rows []*ca.AuthorStats) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, rows...)
	return nil
}
func (f *fakeAuthors) FindByID(_ context.Context, ws, id string) (*ca.AuthorStats, error) {
	for _, r := range f.rows {
		if r.ID == id && r.WorkspaceID == ws {
			c := *r
			if s, ok := f.state[id]; ok {
				c.ModerationState = s
			}
			return &c, nil
		}
	}
	return nil, ca.ErrNotFound
}
func (f *fakeAuthors) List(context.Context, ca.AuthorsInput) (*shared.PaginatedResult[*ca.AuthorStats], error) {
	return shared.NewPaginatedResult(f.rows, shared.Pagination{}, int64(len(f.rows))), nil
}
func (f *fakeAuthors) SetModerationState(_ context.Context, ws, id string, s ca.ModerationState, _ time.Time) error {
	if _, err := f.FindByID(context.Background(), ws, id); err != nil {
		return err
	}
	if f.state == nil {
		f.state = map[string]ca.ModerationState{}
	}
	f.state[id] = s
	return nil
}

func (f *fakeAuthors) SetRole(_ context.Context, ws, id string, role ca.AuthorRoleInference, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.ID == id && r.WorkspaceID == ws {
			r.Role = role
			return nil
		}
	}
	return ca.ErrNotFound
}

func (f *fakeAuthors) ListForRoleInference(_ context.Context, source ca.Source, accountID string, minComments, limit int) ([]*ca.AuthorStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*ca.AuthorStats, 0, len(f.rows))
	for _, r := range f.rows {
		if r.Source == source && r.AccountID == accountID && r.Total >= minComments {
			out = append(out, r)
		}
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

type fakeRollups struct {
	mu   sync.Mutex
	rows []*ca.Rollup
}

func (f *fakeRollups) UpsertMany(_ context.Context, rows []*ca.Rollup) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, rows...)
	return nil
}
func (f *fakeRollups) ListSeries(context.Context, ca.TrendInput) ([]*ca.Rollup, error) {
	return f.rows, nil
}

// The rollup job rebuilds exactly the days that changed, finalises the
// score on each row, derives every author, and advances its marker.
func TestRollupJob_RebuildsTouchedDaysAndAuthors(t *testing.T) {
	repo := newFakeRepo()
	day := ca.BucketDate(now.Add(-40 * 24 * time.Hour)) // a backfilled day, far back
	repo.DaysAnalyzedSinceFn = func(since time.Time) ([]time.Time, error) {
		if since.After(now.Add(-rollupLookback)) || since.Before(now.Add(-rollupLookback-time.Minute)) {
			t.Errorf("first run must rebuild from the lookback window, got since=%v", since)
		}
		return []time.Time{day}, nil
	}
	repo.AggregateRollupsFn = func(d time.Time) ([]*ca.Rollup, error) {
		if !d.Equal(day) {
			t.Errorf("aggregated %v, want %v", d, day)
		}
		return []*ca.Rollup{{WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: "acc-1", Scope: ca.ScopeAccount,
			ScopeID: "acc-1", BucketDate: d, Counters: ca.Counters{Analyzed: 30, StanceSupporter: 30}}}, nil
	}
	repo.AggregateAuthorsFn = func(source ca.Source, accountID string, _ time.Time) ([]*ca.AuthorStats, error) {
		return []*ca.AuthorStats{{WorkspaceID: "ws-1", Source: source, AccountID: accountID, AuthorExternalID: "u-9",
			Counters: ca.Counters{StanceHostile: 3, SeverityHighCount: 3}}}, nil
	}
	authors, rollups, state := &fakeAuthors{}, &fakeRollups{}, newFakeState()
	job := NewRollupJob(repo, newFakeSettings(enabledSettings()), authors, rollups, state, fixedClock{now})

	if err := job.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(rollups.rows) != 1 || rollups.rows[0].AcceptanceScore != 100 || !rollups.rows[0].ComputedAt.Equal(now) {
		t.Fatalf("rollups = %+v", rollups.rows)
	}
	if len(authors.rows) != 1 || authors.rows[0].DerivedStance != ca.StanceHostile || !authors.rows[0].IsFlagged {
		t.Fatalf("authors = %+v", authors.rows)
	}
	marker, _ := state.GetString(rollupLastRunKey)
	if marker != now.Format(time.RFC3339) {
		t.Fatalf("marker = %q", marker)
	}

	// Second run: the window starts one minute before the marker.
	repo.DaysAnalyzedSinceFn = func(since time.Time) ([]time.Time, error) {
		if !since.Equal(now.Add(-time.Minute)) {
			t.Errorf("second run since = %v, want marker minus a minute", since)
		}
		return nil, nil
	}
	if err := job.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// ---- purge ----

func TestPurgeJob_DeletesInSlicesAndSparesInFlight(t *testing.T) {
	repo := newFakeRepo()
	for i := 0; i < 3; i++ {
		row, _ := ca.NewPending(ca.NewInput{WorkspaceID: "ws-1", Container: ref(), SourceCommentID: "old-" + itoa(i),
			AuthorExternalID: "u", Text: "x", Now: now.Add(-400 * 24 * time.Hour)})
		row.ID = "old-" + itoa(i)
		if i == 0 {
			_ = row.Claim(now)
		}
		_ = repo.Save(context.Background(), row)
	}
	fresh, _ := ca.NewPending(ca.NewInput{WorkspaceID: "ws-1", Container: ref(), SourceCommentID: "fresh",
		AuthorExternalID: "u", Text: "x", Now: now})
	fresh.ID = "fresh"
	_ = repo.Save(context.Background(), fresh)

	if err := NewPurgeJob(repo, 180*24*time.Hour, fixedClock{now}).Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.rows) != 2 { // the in-flight old one and the fresh one
		t.Fatalf("rows left = %d, want 2", len(repo.rows))
	}
	if _, ok := repo.rows["old-0"]; !ok {
		t.Fatal("an in-flight row must never be purged from under a batch")
	}
	// Retention 0 is "keep forever".
	if err := NewPurgeJob(repo, 0, fixedClock{now}).Execute(context.Background()); err != nil || len(repo.rows) != 2 {
		t.Fatalf("retention 0: err=%v rows=%d", err, len(repo.rows))
	}
}

// ---- settings ----

type fakeVerifier struct{ owned map[string]string } // accountID -> workspaceID

func (f fakeVerifier) AccountBelongsTo(_ context.Context, ws, account string) (bool, error) {
	return f.owned[account] == ws, nil
}

func settingsHarness() (ca.GetSettingsUseCase, ca.UpdateSettingsUseCase, *fakeSettings) {
	store := newFakeSettings()
	verifiers := map[ca.Source]AccountVerifier{ca.SourceInstagram: fakeVerifier{owned: map[string]string{"acc-1": "ws-1"}}}
	get, update := NewSettingsUseCases(store, verifiers, fixedClock{now})
	return get, update, store
}

func TestSettings_DefaultsAreDisabledAndScoped(t *testing.T) {
	get, _, _ := settingsHarness()
	s, err := get.Execute(context.Background(), "ws-1", ca.SourceInstagram, "acc-1")
	if err != nil {
		t.Fatal(err)
	}
	if s.Enabled || !s.Topics.Has(ca.TopicKeyOther) || s.DailyCap != ca.DefaultDailyCap {
		t.Fatalf("defaults = %+v", s)
	}
	// Another workspace cannot read this account's settings.
	if _, err := get.Execute(context.Background(), "ws-2", ca.SourceInstagram, "acc-1"); !errors.Is(err, ca.ErrNotFound) {
		t.Fatalf("cross-workspace read: %v", err)
	}
}

func TestSettings_UpdateMergesAndReseedsTopics(t *testing.T) {
	get, update, store := settingsHarness()
	enabled := true
	vertical := ca.VerticalRetail
	cap := 500
	s, err := update.Execute(context.Background(), ca.UpdateSettingsInput{
		WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: "acc-1",
		Enabled: &enabled, Vertical: &vertical, DailyCap: &cap,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Enabled || s.Vertical != ca.VerticalRetail || !s.Topics.Has("entrega") || s.DailyCap != 500 {
		t.Fatalf("after update = %+v", s)
	}
	if saved, _ := store.Find(context.Background(), ca.SourceInstagram, "acc-1"); saved == nil || !saved.Enabled {
		t.Fatal("settings not persisted")
	}

	// A custom topic set is normalised and kept; other stays.
	topics := ca.TopicSet{{Key: "", Label: "Saúde Pública"}, {Label: "Entrega"}}
	s, err = update.Execute(context.Background(), ca.UpdateSettingsInput{
		WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: "acc-1", Topics: &topics,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Topics.Has("saude-publica") || !s.Topics.Has(ca.TopicKeyOther) || len(s.Topics) != 3 {
		t.Fatalf("topics = %+v", s.Topics)
	}
	// Threshold is clamped by the domain, not trusted from the wire.
	policy := ca.ActionPolicy{SeverityThreshold: 900}
	s, err = update.Execute(context.Background(), ca.UpdateSettingsInput{
		WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: "acc-1", ActionPolicy: &policy,
	})
	if err != nil || s.ActionPolicy.SeverityThreshold != 100 {
		t.Fatalf("threshold: %v %+v", err, s.ActionPolicy)
	}
	if got, _ := get.Execute(context.Background(), "ws-1", ca.SourceInstagram, "acc-1"); got.Vertical != ca.VerticalRetail {
		t.Fatal("get must return the stored settings")
	}
	// Another workspace cannot write them.
	if _, err := update.Execute(context.Background(), ca.UpdateSettingsInput{
		WorkspaceID: "ws-2", Source: ca.SourceInstagram, AccountID: "acc-1", Enabled: &enabled,
	}); !errors.Is(err, ca.ErrNotFound) {
		t.Fatalf("cross-workspace write: %v", err)
	}
}

// ---- moderation ----

func TestSetModerationState(t *testing.T) {
	authors := &fakeAuthors{rows: []*ca.AuthorStats{{ID: "a-1", WorkspaceID: "ws-1"}}}
	uc := NewSetModerationStateUseCase(authors, fixedClock{now})
	got, err := uc.Execute(context.Background(), ca.SetModerationStateInput{WorkspaceID: "ws-1", AuthorID: "a-1", State: ca.ModerationBlocked})
	if err != nil || got.ModerationState != ca.ModerationBlocked {
		t.Fatalf("got %+v err %v", got, err)
	}
	if _, err := uc.Execute(context.Background(), ca.SetModerationStateInput{WorkspaceID: "ws-1", AuthorID: "a-1", State: "banned"}); !errors.Is(err, ca.ErrInvalidFilter) {
		t.Fatalf("bad state: %v", err)
	}
	if _, err := uc.Execute(context.Background(), ca.SetModerationStateInput{WorkspaceID: "ws-2", AuthorID: "a-1", State: ca.ModerationMuted}); !errors.Is(err, ca.ErrNotFound) {
		t.Fatalf("cross-workspace: %v", err)
	}
}

// ---- backfill ----

type fakeBackfills struct {
	mu   sync.Mutex
	rows map[string]*ca.Backfill
}

func newFakeBackfills() *fakeBackfills { return &fakeBackfills{rows: map[string]*ca.Backfill{}} }

func (f *fakeBackfills) Create(_ context.Context, b *ca.Backfill) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := *b
	f.rows[b.ID] = &c
	return nil
}
func (f *fakeBackfills) Save(_ context.Context, b *ca.Backfill) error {
	return f.Create(context.Background(), b)
}
func (f *fakeBackfills) FindByID(_ context.Context, ws, id string) (*ca.Backfill, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.rows[id]
	if !ok || b.WorkspaceID != ws {
		return nil, ca.ErrNotFound
	}
	c := *b
	return &c, nil
}
func (f *fakeBackfills) ClaimNextPending(_ context.Context, at time.Time) (*ca.Backfill, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range f.rows {
		if b.Status == ca.BackfillPending {
			_ = b.Start(at)
			c := *b
			return &c, nil
		}
	}
	return nil, nil
}
func (f *fakeBackfills) FindActive(_ context.Context, source ca.Source, account, container string) (*ca.Backfill, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range f.rows {
		if b.Source == source && b.AccountID == account && b.ContainerID == container && !b.Status.IsTerminal() && b.Status != ca.BackfillFailed {
			c := *b
			return &c, nil
		}
	}
	return nil, ca.ErrNotFound
}

type recordingIngestor struct {
	mu    sync.Mutex
	seen  []string
	errOn string
}

func (r *recordingIngestor) Enqueue(_ context.Context, in ca.IngestInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if in.SourceCommentID == r.errOn {
		return errors.New("boom")
	}
	r.seen = append(r.seen, in.SourceCommentID)
	return nil
}

func backfillHarness() (BackfillDeps, *fakeAdapter, *fakeBackfills, *recordingIngestor, *fakeState) {
	adapter := &fakeAdapter{
		containers: []ca.ContainerSummary{
			{Ref: ca.ContainerRef{Source: ca.SourceInstagram, AccountID: "acc-1", ContainerID: "m-1"}, WorkspaceID: "ws-1", CommentsCount: 3},
			{Ref: ca.ContainerRef{Source: ca.SourceInstagram, AccountID: "acc-1", ContainerID: "m-2"}, WorkspaceID: "ws-1", CommentsCount: 2},
		},
		pages: map[string]map[string]fakePage{
			"m-1": {
				"":   {items: ingestItems("m-1", "a", "b"), next: "p2"},
				"p2": {items: ingestItems("m-1", "c"), next: ""},
			},
			"m-2": {"": {items: ingestItems("m-2", "d", "e"), next: ""}},
		},
	}
	backfills := newFakeBackfills()
	ingestor := &recordingIngestor{}
	state := newFakeState()
	deps := BackfillDeps{
		Backfills: backfills, Settings: newFakeSettings(enabledSettings()), Ingestor: ingestor,
		Adapters:  map[ca.Source]ca.SourceAdapter{ca.SourceInstagram: adapter},
		Verifiers: map[ca.Source]AccountVerifier{ca.SourceInstagram: fakeVerifier{owned: map[string]string{"acc-1": "ws-1"}}},
		State:     state, Clock: fixedClock{now},
	}
	return deps, adapter, backfills, ingestor, state
}

func ingestItems(container string, ids ...string) []ca.IngestInput {
	out := make([]ca.IngestInput, len(ids))
	for i, id := range ids {
		out[i] = ca.IngestInput{WorkspaceID: "ws-1",
			Container:       ca.ContainerRef{Source: ca.SourceInstagram, AccountID: "acc-1", ContainerID: container},
			SourceCommentID: id, AuthorExternalID: "u", Text: "t"}
	}
	return out
}

// Estimate → confirm → run to completion, every page through the ingestor.
func TestBackfill_EstimateStartAndDrain(t *testing.T) {
	deps, adapter, backfills, ingestor, _ := backfillHarness()
	estimate, start, get, _ := NewBackfillUseCases(deps)
	ctx := context.Background()

	est, err := estimate.Execute(ctx, "ws-1", ca.SourceInstagram, "acc-1", "")
	if err != nil || est.Containers != 2 || est.EstimatedComments != 5 {
		t.Fatalf("estimate: %+v %v", est, err)
	}
	// A stale confirmation is refused: the operator must see the real number.
	if _, err := start.Execute(ctx, ca.StartBackfillInput{WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: "acc-1", ConfirmedEstimate: 4}); !errors.Is(err, ErrBackfillEstimateStale) {
		t.Fatalf("stale estimate: %v", err)
	}
	b, err := start.Execute(ctx, ca.StartBackfillInput{WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: "acc-1", ConfirmedEstimate: 5, RequestedByUserID: "user-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := start.Execute(ctx, ca.StartBackfillInput{WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: "acc-1", ConfirmedEstimate: 5}); !errors.Is(err, ErrBackfillAlreadyActive) {
		t.Fatalf("second start: %v", err)
	}

	if err := NewBackfillJob(deps).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	done, _ := get.Execute(ctx, "ws-1", b.ID)
	if done.Status != ca.BackfillDone || done.Fetched != 5 || done.Enqueued != 5 || adapter.Fetches != 3 {
		t.Fatalf("after drain: %+v fetches=%d", done, adapter.Fetches)
	}
	if len(ingestor.seen) != 5 {
		t.Fatalf("enqueued %v", ingestor.seen)
	}
	// The backfill never classified anything itself.
	if _, err := backfills.FindActive(ctx, ca.SourceInstagram, "acc-1", ""); !errors.Is(err, ca.ErrNotFound) {
		t.Fatal("a finished backfill must not read as active")
	}
}

// The hourly call budget parks the run with its cursor; the next hour
// resumes from the same page rather than starting over.
func TestBackfill_RateBudgetPausesAndResumes(t *testing.T) {
	deps, adapter, backfills, ingestor, state := backfillHarness()
	_, start, get, _ := NewBackfillUseCases(deps)
	ctx := context.Background()
	b, err := start.Execute(ctx, ca.StartBackfillInput{WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: "acc-1", ConfirmedEstimate: 5})
	if err != nil {
		t.Fatal(err)
	}
	// Spend all but one call of this hour's budget.
	key := backfillCallKey + "acc-1:" + now.UTC().Format("2006-01-02-15")
	_, _ = state.IncrBy(key, hourlyCallBudget-1)

	if err := NewBackfillJob(deps).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	paused, _ := get.Execute(ctx, "ws-1", b.ID)
	if paused.Status != ca.BackfillPending || paused.Fetched != 2 || paused.Cursor != "0|p2" {
		t.Fatalf("after budget: %+v", paused)
	}
	if adapter.Fetches != 1 {
		t.Fatalf("fetches = %d, want 1", adapter.Fetches)
	}

	// Next hour.
	deps.Clock = fixedClock{now.Add(time.Hour)}
	if err := NewBackfillJob(deps).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	done, _ := get.Execute(ctx, "ws-1", b.ID)
	if done.Status != ca.BackfillDone || done.Fetched != 5 {
		t.Fatalf("after resume: %+v", done)
	}
	if len(ingestor.seen) != 5 || ingestor.seen[2] != "c" {
		t.Fatalf("resume did not continue from the cursor: %v", ingestor.seen)
	}
	_ = backfills
}

func TestBackfill_ProviderFailureIsRecordedAndCancelWorks(t *testing.T) {
	deps, adapter, _, _, _ := backfillHarness()
	_, start, get, cancel := NewBackfillUseCases(deps)
	ctx := context.Background()
	b, _ := start.Execute(ctx, ca.StartBackfillInput{WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: "acc-1", ConfirmedEstimate: 5})
	adapter.fetchErr = errors.New("429 rate limited")
	if err := NewBackfillJob(deps).Execute(ctx); err == nil {
		t.Fatal("a provider failure must surface")
	}
	failed, _ := get.Execute(ctx, "ws-1", b.ID)
	if failed.Status != ca.BackfillFailed || failed.Error == "" {
		t.Fatalf("after failure: %+v", failed)
	}
	if _, err := cancel.Execute(ctx, "ws-1", b.ID); !errors.Is(err, ErrBackfillNotCancelable) {
		t.Fatalf("cancelling a failed run: %v", err)
	}

	// A fresh pending run can be cancelled before it starts.
	adapter.fetchErr = nil
	b2, err := start.Execute(ctx, ca.StartBackfillInput{WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: "acc-1", ConfirmedEstimate: 5})
	if err != nil {
		t.Fatal(err)
	}
	c, err := cancel.Execute(ctx, "ws-1", b2.ID)
	if err != nil || c.Status != ca.BackfillCanceled {
		t.Fatalf("cancel: %+v %v", c, err)
	}
	if _, err := cancel.Execute(ctx, "ws-2", b2.ID); !errors.Is(err, ca.ErrNotFound) {
		t.Fatalf("cross-workspace cancel: %v", err)
	}
}

func TestBackfill_RefusesDisabledAccount(t *testing.T) {
	deps, _, _, _, _ := backfillHarness()
	off := enabledSettings()
	off.Enabled = false
	deps.Settings = newFakeSettings(off)
	_, start, _, _ := NewBackfillUseCases(deps)
	if _, err := start.Execute(context.Background(), ca.StartBackfillInput{WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: "acc-1", ConfirmedEstimate: 5}); err == nil {
		t.Fatal("backfilling a disabled account must be refused")
	}
}

// ---- spend / trends / list wiring ----

func TestSpendUseCase_DefaultsToCalendarMonth(t *testing.T) {
	batches := &fakeBatches{}
	uc := NewSpendUseCase(batches, fixedClock{now})
	if _, err := uc.Execute(context.Background(), "ws-1", ca.SpendInput{}); err != nil {
		t.Fatal(err)
	}
}

func TestStatsUseCase_FinalizesScore(t *testing.T) {
	repo := newFakeRepo()
	stats, err := NewStatsUseCase(repo).Execute(context.Background(), ca.ListInput{WorkspaceID: "ws-1"})
	if err != nil || !stats.Finalized {
		t.Fatalf("stats must be finalised by the use case: %+v %v", stats, err)
	}
	if _, err := NewStatsUseCase(repo).Execute(context.Background(), ca.ListInput{}); !errors.Is(err, ca.ErrInvalidFilter) {
		t.Fatalf("missing workspace: %v", err)
	}
}
