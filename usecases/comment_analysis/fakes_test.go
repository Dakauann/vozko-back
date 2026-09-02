package comment_analysis_usecase

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"vozko/domain/ai"
	ca "vozko/domain/comment_analysis"
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
	rows map[string]*ca.CommentAnalysis
	// Saved counts SaveMany calls, so a test can assert persistence happened.
	Saved     int
	InsertErr error

	AggregateAuthorsFn  func(source ca.Source, accountID string, since time.Time) ([]*ca.AuthorStats, error)
	AggregateRollupsFn  func(day time.Time) ([]*ca.Rollup, error)
	DaysAnalyzedSinceFn func(since time.Time) ([]time.Time, error)
}

func newFakeRepo() *fakeRepo { return &fakeRepo{rows: map[string]*ca.CommentAnalysis{}} }

func (f *fakeRepo) clone(a *ca.CommentAnalysis) *ca.CommentAnalysis {
	c := *a
	return &c
}

func (f *fakeRepo) Insert(_ context.Context, a *ca.CommentAnalysis) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.InsertErr != nil {
		return false, f.InsertErr
	}
	for _, r := range f.rows {
		if r.Source == a.Source && r.SourceCommentID == a.SourceCommentID {
			return false, nil
		}
	}
	f.rows[a.ID] = f.clone(a)
	return true, nil
}

func (f *fakeRepo) FindByID(_ context.Context, workspaceID, id string) (*ca.CommentAnalysis, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok || r.WorkspaceID != workspaceID {
		return nil, ca.ErrNotFound
	}
	return f.clone(r), nil
}

func (f *fakeRepo) FindBySourceComment(_ context.Context, source ca.Source, id string) (*ca.CommentAnalysis, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.Source == source && r.SourceCommentID == id {
			return f.clone(r), nil
		}
	}
	return nil, ca.ErrNotFound
}

func (f *fakeRepo) ListPending(_ context.Context, ref ca.ContainerRef, limit int) ([]*ca.CommentAnalysis, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*ca.CommentAnalysis
	for _, r := range f.rows {
		if r.Status == ca.StatusPending && r.DeletedAt == nil && r.Container() == ref {
			out = append(out, f.clone(r))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].SourceCommentID < out[j].SourceCommentID
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

func (f *fakeRepo) Save(_ context.Context, a *ca.CommentAnalysis) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[a.ID] = f.clone(a)
	return nil
}

func (f *fakeRepo) SaveMany(ctx context.Context, rows []*ca.CommentAnalysis) error {
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

func (f *fakeRepo) ListStaleInFlight(_ context.Context, before time.Time, limit int) ([]*ca.CommentAnalysis, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*ca.CommentAnalysis
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

func (f *fakeRepo) List(context.Context, ca.ListInput) (*shared.PaginatedResult[*ca.CommentAnalysis], error) {
	return shared.NewPaginatedResult([]*ca.CommentAnalysis{}, shared.Pagination{}, 0), nil
}
func (f *fakeRepo) GetStats(context.Context, ca.ListInput) (*ca.Stats, error) {
	return &ca.Stats{}, nil
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
func (f *fakeRepo) bySourceID(id string) *ca.CommentAnalysis {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.SourceCommentID == id {
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
	return ca.ContainerContext{Caption: f.caption}, nil
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

// ---- charger ----

type fakeCharger struct {
	mu       sync.Mutex
	reserved int
	capLeft  int // -1 = unlimited
	charges  []int
	price    int64
}

func newFakeCharger() *fakeCharger { return &fakeCharger{capLeft: -1} }

func (f *fakeCharger) ReserveDaily(_ context.Context, _ string, items, _ int, _ time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.capLeft >= 0 && f.reserved+items > f.capLeft {
		return false, nil
	}
	f.reserved += items
	return true, nil
}
func (f *fakeCharger) ChargeBatch(_ context.Context, _, _ string, items int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.charges = append(f.charges, items)
	return f.price * int64(items), nil
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
func (s *fakeState) HIncrBy(string, string, int64) (int64, error) { return 0, nil }
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
	repo       *fakeRepo
	settings   *fakeSettings
	batches    *fakeBatches
	adapter    *fakeAdapter
	classifier *fakeClassifier
	scheduler  *fakeScheduler
	charger    *fakeCharger
	balance    *fakeBalance
	state      *fakeState
	engine     *Engine
}

func newHarness(t interface{ Fatal(...any) }, budget ca.Budget) *harness {
	h := &harness{
		repo:       newFakeRepo(),
		settings:   newFakeSettings(enabledSettings()),
		batches:    &fakeBatches{},
		adapter:    &fakeAdapter{texts: map[string]string{}, caption: "Asfalto novo na Rua A"},
		classifier: &fakeClassifier{},
		scheduler:  newFakeScheduler(),
		charger:    newFakeCharger(),
		balance:    &fakeBalance{micros: 1_000_000},
		state:      newFakeState(),
	}
	engine, err := NewEngine(EngineDeps{
		Repo: h.repo, Settings: NewSettingsResolver(h.settings), Batches: h.batches,
		Adapters:   map[ca.Source]ca.SourceAdapter{ca.SourceInstagram: h.adapter},
		Classifier: h.classifier, Scheduler: h.scheduler, Charger: h.charger,
		Balance: h.balance, State: h.state, Clock: fixedClock{now}, Budget: budget,
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
			WorkspaceID: "ws-1", Container: ref(), SourceCommentID: id, AuthorExternalID: "u-" + itoa(i%5),
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
