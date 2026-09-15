package audience_usecase

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"vozko/domain/ai"
	ca "vozko/domain/audience"
	"vozko/domain/shared"
)

// Hand-written fakes with function fields, in the style of
// usecases/instagram/fakes_test.go. The repository fake is a real in-memory
// implementation of the status machine's persistence, because the engine
// tests are about what ends up in the rows.

var now = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func ref() ca.ContainerRef {
	return ca.ContainerRef{Source: ca.SourceInstagram, AccountID: "acc-1", ContainerID: "media-1"}
}

// ---- repository ----

type fakeRepo struct {
	mu   sync.Mutex
	rows map[string]*ca.Analysis
	// Saved counts SaveMany calls, so a test can assert persistence happened.
	Saved     int
	InsertErr error
	// failCountWaiting makes the backlog read fail, so a test can pin that the
	// budget still reports its ceiling when the queue cannot be counted.
	failCountWaiting bool

	AggregateAuthorsFn     func(source ca.Source, accountID string, since time.Time) ([]*ca.AuthorStats, error)
	AggregateRollupsFn     func(day time.Time) ([]*ca.Rollup, error)
	DaysAnalyzedSinceFn    func(since time.Time) ([]time.Time, error)
	ListAuthorContainersFn func(in ca.AuthorContainersInput) (*shared.PaginatedResult[*ca.AuthorContainer], error)
}

func newFakeRepo() *fakeRepo { return &fakeRepo{rows: map[string]*ca.Analysis{}} }

func (f *fakeRepo) clone(a *ca.Analysis) *ca.Analysis {
	c := *a
	return &c
}

func (f *fakeRepo) Insert(_ context.Context, a *ca.Analysis) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.InsertErr != nil {
		return false, f.InsertErr
	}
	for _, r := range f.rows {
		if r.Source == a.Source && r.Kind() == a.Kind() && r.SubjectID == a.SubjectID && r.Revision == a.Revision {
			return false, nil
		}
	}
	f.rows[a.ID] = f.clone(a)
	return true, nil
}

func (f *fakeRepo) FindByID(_ context.Context, workspaceID, id string) (*ca.Analysis, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok || r.WorkspaceID != workspaceID {
		return nil, ca.ErrNotFound
	}
	return f.clone(r), nil
}

func (f *fakeRepo) FindBySourceComment(_ context.Context, source ca.Source, id string) (*ca.Analysis, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.Source == source && r.SubjectID == id {
			return f.clone(r), nil
		}
	}
	return nil, ca.ErrNotFound
}

func (f *fakeRepo) ListPending(_ context.Context, ref ca.ContainerRef, limit int) ([]*ca.Analysis, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*ca.Analysis
	for _, r := range f.rows {
		if r.Status == ca.StatusPending && r.DeletedAt == nil && r.Container().Equal(ref) {
			out = append(out, f.clone(r))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].SubjectID < out[j].SubjectID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeRepo) ClaimByIDs(_ context.Context, ids []string, at time.Time) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var claimed []string
	for _, id := range ids {
		r, ok := f.rows[id]
		if !ok || r.Status != ca.StatusPending {
			continue
		}
		_ = r.Claim(at)
		claimed = append(claimed, id)
	}
	return claimed, nil
}

func (f *fakeRepo) Save(_ context.Context, a *ca.Analysis) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[a.ID] = f.clone(a)
	return nil
}

func (f *fakeRepo) SaveMany(ctx context.Context, rows []*ca.Analysis) error {
	f.mu.Lock()
	f.Saved++
	f.mu.Unlock()
	for _, a := range rows {
		if err := f.Save(ctx, a); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeRepo) ListPendingContainers(_ context.Context, olderThan time.Time, limit int) ([]ca.PendingContainer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := map[ca.ContainerRef]*ca.PendingContainer{}
	for _, r := range f.rows {
		if r.Status != ca.StatusPending || !r.CreatedAt.Before(olderThan) {
			continue
		}
		c := seen[r.Container()]
		if c == nil {
			c = &ca.PendingContainer{Ref: r.Container(), WorkspaceID: r.WorkspaceID, OldestAt: r.CreatedAt}
			seen[r.Container()] = c
		}
		c.Pending++
	}
	var out []ca.PendingContainer
	for _, c := range seen {
		out = append(out, *c)
	}
	return out, nil
}

func (f *fakeRepo) ListStaleInFlight(_ context.Context, before time.Time, limit int) ([]*ca.Analysis, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*ca.Analysis
	for _, r := range f.rows {
		if r.Status == ca.StatusInFlight && r.UpdatedAt.Before(before) {
			out = append(out, f.clone(r))
		}
	}
	return out, nil
}

func (f *fakeRepo) CountPendingBySource(context.Context) (map[ca.Source]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[ca.Source]int{}
	for _, r := range f.rows {
		if r.Status == ca.StatusPending {
			out[r.Source]++
		}
	}
	return out, nil
}

// CountWaiting answers ca.BacklogReader from the rows the fake already holds,
// so a test that queues work sees the backlog without a second source of truth.
func (f *fakeRepo) CountWaiting(_ context.Context, workspaceID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failCountWaiting {
		return 0, errors.New("backlog unavailable")
	}
	n := 0
	for _, r := range f.rows {
		if r.WorkspaceID != workspaceID {
			continue
		}
		if r.Status == ca.StatusPending || r.Status == ca.StatusInFlight {
			n++
		}
	}
	return n, nil
}

// seedWaiting puts n unclassified rows in one workspace's queue.
func (f *fakeRepo) seedWaiting(workspaceID string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%s-waiting-%d", workspaceID, i)
		f.rows[id] = &ca.Analysis{
			ID: id, WorkspaceID: workspaceID, Source: ca.SourceInstagram,
			Status: ca.StatusPending,
		}
	}
}

// List filters the stored rows on the fields the use cases actually pass. It
// used to return an empty page unconditionally, which made anything reading a
// corpus back (the author pass) silently see nothing and pass for the wrong
// reason.
func (f *fakeRepo) List(_ context.Context, in ca.ListInput) (*shared.PaginatedResult[*ca.Analysis], error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	statuses := map[ca.Status]bool{}
	for _, s := range in.Statuses {
		statuses[s] = true
	}

	out := make([]*ca.Analysis, 0, len(f.rows))
	for _, r := range f.rows {
		if in.WorkspaceID != "" && r.WorkspaceID != in.WorkspaceID {
			continue
		}
		if in.Source != "" && r.Source != in.Source {
			continue
		}
		if in.AccountID != "" && r.AccountID != in.AccountID {
			continue
		}
		if in.ContainerID != "" && r.ContainerID != in.ContainerID {
			continue
		}
		if in.AuthorExternalID != "" && r.AuthorExternalID != in.AuthorExternalID {
			continue
		}
		if len(statuses) > 0 && !statuses[r.Status] {
			continue
		}
		out = append(out, f.clone(r))
	}

	total := int64(len(out))
	pagination := shared.NormalizePagination(in.Options.Pagination)
	if start := pagination.Offset(); start < len(out) {
		end := start + pagination.PageSize
		if end > len(out) {
			end = len(out)
		}
		out = out[start:end]
	} else {
		out = nil
	}
	return shared.NewPaginatedResult(out, pagination, total), nil
}
func (f *fakeRepo) ListAuthorContainers(_ context.Context, in ca.AuthorContainersInput) (*shared.PaginatedResult[*ca.AuthorContainer], error) {
	if f.ListAuthorContainersFn != nil {
		return f.ListAuthorContainersFn(in)
	}
	return shared.NewPaginatedResult([]*ca.AuthorContainer{}, in.Options.Pagination, 0), nil
}
func (f *fakeRepo) GetStats(context.Context, ca.ListInput) (*ca.Stats, error) {
	return &ca.Stats{}, nil
}

func (f *fakeRepo) GetTrend(context.Context, ca.ListInput) ([]*ca.Rollup, error) {
	return []*ca.Rollup{}, nil
}
func (f *fakeRepo) AggregateAuthors(_ context.Context, source ca.Source, accountID string, since time.Time) ([]*ca.AuthorStats, error) {
	if f.AggregateAuthorsFn != nil {
		return f.AggregateAuthorsFn(source, accountID, since)
	}
	return nil, nil
}
func (f *fakeRepo) AggregateRollups(_ context.Context, day time.Time) ([]*ca.Rollup, error) {
	if f.AggregateRollupsFn != nil {
		return f.AggregateRollupsFn(day)
	}
	return nil, nil
}
func (f *fakeRepo) DaysAnalyzedSince(_ context.Context, since time.Time) ([]time.Time, error) {
	if f.DaysAnalyzedSinceFn != nil {
		return f.DaysAnalyzedSinceFn(since)
	}
	return nil, nil
}
func (f *fakeRepo) SoftDeleteBySourceComment(context.Context, ca.Source, string, time.Time) error {
	return nil
}
func (f *fakeRepo) PurgeBefore(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for id, r := range f.rows {
		if n >= int64(limit) {
			break
		}
		if r.CreatedAt.Before(cutoff) && r.Status != ca.StatusInFlight {
			delete(f.rows, id)
			n++
		}
	}
	return n, nil
}

// helpers for assertions
func (f *fakeRepo) bySourceID(id string) *ca.Analysis {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.SubjectID == id {
			return f.clone(r)
		}
	}
	return nil
}

func (f *fakeRepo) countStatus(s ca.Status) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.rows {
		if r.Status == s {
			n++
		}
	}
	return n
}

// ---- settings ----

type fakeSettings struct {
	byAccount map[string]*ca.Settings
	overrides map[string]*ca.ContainerOverride
}

func newFakeSettings(s ...*ca.Settings) *fakeSettings {
	f := &fakeSettings{byAccount: map[string]*ca.Settings{}, overrides: map[string]*ca.ContainerOverride{}}
	for _, x := range s {
		f.byAccount[string(x.Source)+":"+x.AccountID] = x
	}
	return f
}

func (f *fakeSettings) Find(_ context.Context, source ca.Source, accountID string) (*ca.Settings, error) {
	s, ok := f.byAccount[string(source)+":"+accountID]
	if !ok {
		return nil, ca.ErrNotFound
	}
	c := *s
	return &c, nil
}
func (f *fakeSettings) Save(_ context.Context, s *ca.Settings) error {
	c := *s
	f.byAccount[string(s.Source)+":"+s.AccountID] = &c
	return nil
}
func (f *fakeSettings) ListEnabled(context.Context) ([]*ca.Settings, error) {
	var out []*ca.Settings
	for _, s := range f.byAccount {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out, nil
}

func enabledSettings() *ca.Settings {
	s := ca.NewSettings("ws-1", ca.SourceInstagram, "acc-1", ca.VerticalGov)
	s.Enabled = true
	return &s
}

// ---- batches ----

type fakeBatches struct {
	mu   sync.Mutex
	rows []ca.Batch
}

func (f *fakeBatches) Create(_ context.Context, b *ca.Batch) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, *b)
	return nil
}
func (f *fakeBatches) Totals(context.Context, string, time.Time, time.Time) (*ca.BatchTotals, error) {
	return &ca.BatchTotals{}, nil
}

// ---- adapter ----

type fakeAdapter struct {
	texts   map[string]string
	caption string

	permalink    string
	containerErr error

	containers []ca.ContainerSummary
	// pages maps containerID -> cursor -> (items, next cursor)
	pages    map[string]map[string]fakePage
	fetchErr error
	Fetches  int
}

type fakePage struct {
	items []ca.IngestInput
	next  string
}

func (f *fakeAdapter) ReadTexts(_ context.Context, _ ca.ContainerRef, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		if t, ok := f.texts[id]; ok {
			out[id] = t
		}
	}
	return out, nil
}
func (f *fakeAdapter) ReadContainerContext(context.Context, ca.ContainerRef) (ca.ContainerContext, error) {
	if f.containerErr != nil {
		return ca.ContainerContext{}, f.containerErr
	}
	return ca.ContainerContext{Caption: f.caption, Permalink: f.permalink}, nil
}
func (f *fakeAdapter) ListContainers(_ context.Context, _ string, limit, offset int) ([]ca.ContainerSummary, error) {
	if offset >= len(f.containers) {
		return nil, nil
	}
	end := offset + limit
	if end > len(f.containers) {
		end = len(f.containers)
	}
	return f.containers[offset:end], nil
}
func (f *fakeAdapter) FetchCommentsPage(_ context.Context, ref ca.ContainerRef, cursor string) ([]ca.IngestInput, string, error) {
	f.Fetches++
	if f.fetchErr != nil {
		return nil, "", f.fetchErr
	}
	p := f.pages[ref.ContainerID][cursor]
	return p.items, p.next, nil
}

// ---- classifier ----

// fakeClassifier answers by script: each call pops the next response. Calls
// are recorded so a test can assert what was sent (and that nothing was).
type fakeClassifier struct {
	mu        sync.Mutex
	Calls     []ca.ClassifyRequest
	responses []func(req ca.ClassifyRequest) (*ca.ClassifyResult, error)
}

func (f *fakeClassifier) push(fn func(req ca.ClassifyRequest) (*ca.ClassifyResult, error)) {
	f.responses = append(f.responses, fn)
}

// good answers every ref with a valid classification.
func good(req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
	res := &ca.ClassifyResult{FinishReason: "stop", Model: "test-model", PromptTokens: 100, CompletionTokens: 10 * len(req.Batch.Items)}
	for _, it := range req.Batch.Items {
		res.Results = append(res.Results, okResult(it.Ref))
	}
	return res, nil
}

func okResult(ref int) ca.BatchResult {
	return ca.BatchResult{Ref: ref, Sentiment: "positive", Stance: "supporter", Intent: "praise", TopicKey: "saude",
		Language: "pt", Toxicity: "none", PersonalAttack: "none", LegalRisk: "none"}
}

func hostileResult(ref int) ca.BatchResult {
	return ca.BatchResult{Ref: ref, Sentiment: "negative", Stance: "hostile", Intent: "other", TopicKey: "other",
		Language: "pt", Toxicity: "high", PersonalAttack: "high", LegalRisk: "none"}
}

func (f *fakeClassifier) Classify(_ context.Context, req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, req)
	if len(f.responses) == 0 {
		return good(req)
	}
	fn := f.responses[0]
	f.responses = f.responses[1:]
	return fn(req)
}

// ---- scheduler ----

type fakeScheduler struct {
	mu    sync.Mutex
	hints map[string]ca.Hint
}

func newFakeScheduler() *fakeScheduler { return &fakeScheduler{hints: map[string]ca.Hint{}} }

func (f *fakeScheduler) Stamp(_ context.Context, ref ca.ContainerRef, ws string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hints[ref.Key()] = f.hints[ref.Key()].Stamp(ref, ws, at)
	return nil
}
func (f *fakeScheduler) Hints(context.Context) ([]ca.Hint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []ca.Hint
	for _, h := range f.hints {
		out = append(out, h)
	}
	return out, nil
}
func (f *fakeScheduler) Clear(_ context.Context, ref ca.ContainerRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.hints, ref.Key())
	return nil
}


// ---- balance ----

type fakeBalance struct{ micros int64 }

func (f *fakeBalance) HasSufficientBalance(string, int64) (bool, error) { return f.micros > 0, nil }
func (f *fakeBalance) GetBalance(string) (int64, error)                 { return f.micros, nil }
func (f *fakeBalance) Invalidate(string)                                {}
func (f *fakeBalance) InvalidateDebounced(string)                       {}

// ---- shared state (Redis) ----

type fakeState struct {
	mu   sync.Mutex
	kv   map[string]string
	hash map[string]map[string]string
	ctr  map[string]int64
}

func newFakeState() *fakeState {
	return &fakeState{kv: map[string]string{}, hash: map[string]map[string]string{}, ctr: map[string]int64{}}
}

func (s *fakeState) SetNX(key, value string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.kv[key]; ok {
		return false, nil
	}
	s.kv[key] = value
	return true, nil
}
func (s *fakeState) SetString(key, value string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kv[key] = value
	return nil
}
func (s *fakeState) GetString(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kv[key], nil
}
func (s *fakeState) Del(keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		delete(s.kv, k)
	}
	return nil
}
func (s *fakeState) Exists(key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.kv[key]
	return ok, nil
}
func (s *fakeState) Incr(key string) (int64, error) { return s.IncrBy(key, 1) }
func (s *fakeState) Decr(key string) (int64, error) { return s.IncrBy(key, -1) }
func (s *fakeState) IncrWithTTL(key string, _ time.Duration) (int64, error) {
	return s.IncrBy(key, 1)
}
func (s *fakeState) TryIncr(key string, max int64) (bool, error)     { return s.TryIncrBy(key, 1, max) }
func (s *fakeState) SAdd(string, ...string) error                    { return nil }
func (s *fakeState) SRem(string, ...string) error                    { return nil }
func (s *fakeState) SMembers(string) ([]string, error)               { return nil, nil }
func (s *fakeState) Publish(string, []byte) error                    { return nil }
func (s *fakeState) Subscribe(context.Context, string, func([]byte)) {}
func (s *fakeState) HSet(key, field, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hash[key] == nil {
		s.hash[key] = map[string]string{}
	}
	s.hash[key][field] = value
	return nil
}
func (s *fakeState) HDel(key, field string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.hash[key], field)
	return nil
}
func (s *fakeState) HGetAll(key string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for k, v := range s.hash[key] {
		out[k] = v
	}
	return out, nil
}

// A real implementation, not a stub returning zero: the rolling usage budget
// is stored as a hash of hourly buckets, so a no-op here would let every test
// of it pass against a budget that was never spent.
func (s *fakeState) HIncrBy(key, field string, n int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hash[key] == nil {
		s.hash[key] = map[string]string{}
	}
	current, _ := strconv.ParseInt(s.hash[key][field], 10, 64)
	current += n
	s.hash[key][field] = strconv.FormatInt(current, 10)
	return current, nil
}
func (s *fakeState) IncrBy(key string, n int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctr[key] += n
	return s.ctr[key], nil
}
func (s *fakeState) DecrBy(key string, n int64) (int64, error) { return s.IncrBy(key, -n) }
func (s *fakeState) TryIncrBy(key string, delta, max int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctr[key]+delta > max {
		return false, nil
	}
	s.ctr[key] += delta
	return true, nil
}
func (s *fakeState) Expire(string, time.Duration) (bool, error) { return true, nil }

// ---- AI service (for the classifier tests) ----

type fakeAI struct {
	mu     sync.Mutex
	Inputs []ai.GenerateInput
	Output *ai.GenerateOutput
	Err    error
}

func (f *fakeAI) Generate(_ context.Context, in ai.GenerateInput) (*ai.GenerateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Inputs = append(f.Inputs, in)
	if f.Err != nil {
		return nil, f.Err
	}
	if f.Output != nil {
		return f.Output, nil
	}
	return &ai.GenerateOutput{Message: ai.Message{Role: ai.RoleAssistant, Content: `{"results":[]}`}, FinishReason: "stop"}, nil
}
func (f *fakeAI) GenerateStream(context.Context, ai.GenerateInput) (<-chan ai.StreamEvent, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeAI) GetAvaibleModels(context.Context) ([]string, error)           { return nil, nil }
func (f *fakeAI) GetModelsWithPricing(context.Context) ([]ai.ModelInfo, error) { return nil, nil }

// ---- test harness ----

type harness struct {
	repo            *fakeRepo
	settings        *fakeSettings
	workspaceLimits *fakeWorkspaceLimits
	batches         *fakeBatches
	adapter         *fakeAdapter
	classifier      *fakeClassifier
	scheduler       *fakeScheduler
	balance         *fakeBalance
	state           *fakeState
	broadcaster     *fakeBroadcaster
	engine          *Engine
}

// fakeBroadcaster records what the live feed would have been sent.
type fakeBroadcaster struct {
	mu     sync.Mutex
	events []ca.AnalysisBatchAnalyzed
}

func (f *fakeBroadcaster) BroadcastCommentsAnalyzed(e ca.AnalysisBatchAnalyzed) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
}

func (f *fakeBroadcaster) all() []ca.AnalysisBatchAnalyzed {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ca.AnalysisBatchAnalyzed(nil), f.events...)
}

func newHarness(t interface{ Fatal(...any) }, budget ca.Budget) *harness {
	h := &harness{
		repo:            newFakeRepo(),
		settings:        newFakeSettings(enabledSettings()),
		workspaceLimits: newFakeWorkspaceLimits(),
		batches:         &fakeBatches{},
		adapter:         &fakeAdapter{texts: map[string]string{}, caption: "Asfalto novo na Rua A"},
		classifier:      &fakeClassifier{},
		scheduler:       newFakeScheduler(),
		balance:         &fakeBalance{micros: 1_000_000},
		state:           newFakeState(),
		broadcaster:     &fakeBroadcaster{},
	}
	engine, err := NewEngine(EngineDeps{
		Repo: h.repo, Settings: NewSettingsResolver(h.settings), Batches: h.batches,
		Adapters:   map[ca.Source]ca.SourceAdapter{ca.SourceInstagram: h.adapter},
		Classifier: h.classifier, Scheduler: h.scheduler,
		// The REAL limiter over the fake state: the budget is the thing under
		// test in the cap cases, and a fake of it would only prove the fake.
		Usage:           NewUsageLimiter(h.state),
		WorkspaceLimits: h.workspaceLimits,
		Balance:         h.balance, State: h.state, Clock: fixedClock{now}, Budget: budget,
		Broadcaster: h.broadcaster,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.engine = engine
	return h
}

// seed inserts n pending comments c-1..c-n with text and a stamp.
func (h *harness) seed(n int) {
	for i := 1; i <= n; i++ {
		id := "c-" + itoa(i)
		text := "comentário " + itoa(i)
		h.adapter.texts[id] = text
		row, _ := ca.NewPending(ca.NewInput{
			WorkspaceID: "ws-1", Container: ref(), SubjectID: id, AuthorExternalID: "u-" + itoa(i%5),
			Text: text, Now: now.Add(time.Duration(i) * time.Second),
		})
		row.ID = "row-" + itoa(i)
		_, _ = h.repo.Insert(context.Background(), row)
	}
	_ = h.scheduler.Stamp(context.Background(), ref(), "ws-1", now.Add(-time.Hour))
}

func itoa(i int) string { return strconv.Itoa(i) }

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func (f *fakeSettings) ListByWorkspace(_ context.Context, ws string) ([]*ca.Settings, error) {
	var out []*ca.Settings
	for _, s := range f.byAccount {
		if s.WorkspaceID == ws {
			out = append(out, s)
		}
	}
	return out, nil
}
func (f *fakeSettings) FindOverride(_ context.Context, ref ca.ContainerRef) (*ca.ContainerOverride, error) {
	o, ok := f.overrides[ref.Key()]
	if !ok {
		return nil, ca.ErrNotFound
	}
	c := *o
	return &c, nil
}
func (f *fakeSettings) SaveOverride(_ context.Context, o *ca.ContainerOverride) error {
	c := *o
	f.overrides[o.Ref().Key()] = &c
	return nil
}
func (f *fakeSettings) DeleteOverride(_ context.Context, ref ca.ContainerRef) error {
	delete(f.overrides, ref.Key())
	return nil
}
func (f *fakeSettings) ListOverrides(_ context.Context, source ca.Source, accountID string) ([]*ca.ContainerOverride, error) {
	var out []*ca.ContainerOverride
	for _, o := range f.overrides {
		if o.Source == source && o.AccountID == accountID {
			out = append(out, o)
		}
	}
	return out, nil
}

func (f *fakeRepo) LatestBySubject(_ context.Context, workspaceID string, source ca.Source, kind ca.SubjectKind, subjectID string) (*ca.Analysis, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var latest *ca.Analysis
	for _, r := range f.rows {
		if r.SubjectID != subjectID || r.Kind() != kind {
			continue
		}
		if source != "" && r.Source != source {
			continue
		}
		if workspaceID != "" && r.WorkspaceID != workspaceID {
			continue
		}
		if latest == nil || !r.OccurredAt.Before(latest.OccurredAt) {
			latest = r
		}
	}
	return latest, nil
}
