package audience_usecase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	ca "vozko/domain/audience"
	"vozko/domain/shared"
)

// ---- fakes ----

type fakeAlertRules struct {
	mu    sync.Mutex
	rules []*ca.AlertRule
	// claimed counts successful claims, and claimLimit makes the fake behave
	// like the real conditional write: only the first caller wins.
	claimed    int
	claimLimit int
	claimErr   error
	failures   []string
	listErr    error
	// created and updated record the writes, so a test can assert that a
	// refused rule never reached the repository at all.
	created *ca.AlertRule
	updated *ca.AlertRule
}

func (f *fakeAlertRules) Create(_ context.Context, r *ca.AlertRule) error {
	f.created = r
	return nil
}
func (f *fakeAlertRules) Update(_ context.Context, r *ca.AlertRule) error {
	f.updated = r
	return nil
}
func (f *fakeAlertRules) Delete(context.Context, string, string) error {
	return nil
}
func (f *fakeAlertRules) FindByID(_ context.Context, ws, id string) (*ca.AlertRule, error) {
	for _, r := range f.rules {
		if r.ID == id && r.WorkspaceID == ws {
			return r, nil
		}
	}
	return nil, ca.ErrNotFound
}
func (f *fakeAlertRules) ListByAccount(context.Context, string, ca.Source, string) ([]*ca.AlertRule, error) {
	return f.rules, nil
}
func (f *fakeAlertRules) ListArmed(context.Context, ca.Source, string) ([]*ca.AlertRule, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]*ca.AlertRule, 0, len(f.rules))
	for _, r := range f.rules {
		if r.Enabled {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeAlertRules) ClaimFire(_ context.Context, _, _ string, _ time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claimErr != nil {
		return false, f.claimErr
	}
	if f.claimLimit > 0 && f.claimed >= f.claimLimit {
		return false, nil
	}
	f.claimed++
	return true, nil
}
func (f *fakeAlertRules) RecordFailure(_ context.Context, _, _, message string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures = append(f.failures, message)
	return nil
}

type fakeDispatcher struct {
	mu   sync.Mutex
	sent []ca.AlertDelivery
	err  error
}

func (f *fakeDispatcher) Dispatch(_ context.Context, in ca.AlertDelivery) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, in)
	return nil
}

func (f *fakeDispatcher) all() []ca.AlertDelivery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ca.AlertDelivery(nil), f.sent...)
}

// ---- fixtures ----

func severityRule() *ca.AlertRule {
	r := &ca.AlertRule{
		ID: "rule-1", WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: ref().AccountID,
		Name: "Comentário grave", Metric: ca.AlertMetricCommentSeverity, Threshold: 80,
		Channel: ca.AlertChannelUnofficial, Recipient: "5511999999999", Enabled: true,
	}
	r.Normalize()
	return r
}

func analysedAt(id string, severity int, at time.Time) *ca.Analysis {
	return &ca.Analysis{
		ID: id, WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: ref().AccountID,
		ContainerID: ref().ContainerID, SubjectID: id,
		AuthorExternalID: "ig-9", AuthorHandle: "fulano",
		Status: ca.StatusAnalyzed, Stance: ca.StanceHostile, Severity: severity,
		Excerpt: "vocês são uns ladrões", OccurredAt: at,
	}
}

func newAlertEvaluator(rules *fakeAlertRules, dispatcher *fakeDispatcher, repo ca.Repository) ca.AlertEvaluator {
	return NewAlertEvaluator(AlertDeps{
		Rules: rules, Repo: repo, Dispatcher: dispatcher, Clock: fixedClock{now},
	})
}

// ---- per-comment metric ----

func TestAlertFiresOnASevereComment(t *testing.T) {
	rules := &fakeAlertRules{rules: []*ca.AlertRule{severityRule()}}
	dispatcher := &fakeDispatcher{}
	uc := newAlertEvaluator(rules, dispatcher, newFakeRepo())

	uc.EvaluateBatch(context.Background(), ref(), "ws-1", []*ca.Analysis{
		analysedAt("c-1", 20, now),
		analysedAt("c-2", 92, now),
	})

	sent := dispatcher.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d alerts, want 1", len(sent))
	}
	got := sent[0]
	if got.Channel != ca.AlertChannelUnofficial || got.Recipient != "5511999999999" {
		t.Fatalf("delivery = %+v", got)
	}
	// The WORST comment of the batch, not an arbitrary one: reporting the
	// milder comment would understate what actually happened.
	if !strings.Contains(got.Text, "92") {
		t.Fatalf("text should report the worst comment:\n%s", got.Text)
	}
	if got.IdempotencyKey == "" {
		t.Fatal("an alert without an idempotency key can be sent twice")
	}
}

func TestAlertStaysQuietBelowTheThreshold(t *testing.T) {
	rules := &fakeAlertRules{rules: []*ca.AlertRule{severityRule()}}
	dispatcher := &fakeDispatcher{}
	uc := newAlertEvaluator(rules, dispatcher, newFakeRepo())

	uc.EvaluateBatch(context.Background(), ref(), "ws-1", []*ca.Analysis{analysedAt("c-1", 79, now)})
	if len(dispatcher.all()) != 0 {
		t.Fatal("below the threshold is not an alert")
	}
	if rules.claimed != 0 {
		t.Fatal("a rule that will not fire must not be claimed")
	}
}

// THE backfill guard. Importing three months of history classifies thousands
// of old comments; none of them is news, and none may wake anybody up.
func TestAlertIgnoresBackfilledComments(t *testing.T) {
	rules := &fakeAlertRules{rules: []*ca.AlertRule{severityRule()}}
	dispatcher := &fakeDispatcher{}
	uc := newAlertEvaluator(rules, dispatcher, newFakeRepo())

	old := now.Add(-AlertFreshness - time.Hour)
	uc.EvaluateBatch(context.Background(), ref(), "ws-1", []*ca.Analysis{
		analysedAt("c-old-1", 100, old),
		analysedAt("c-old-2", 95, old),
	})

	if len(dispatcher.all()) != 0 {
		t.Fatal("a comment from months ago must not page anybody tonight")
	}
	if rules.claimed != 0 {
		t.Fatal("nothing to claim for a batch of old comments")
	}
}

// A disabled rule is never even listed, let alone claimed.
func TestAlertSkipsDisabledRules(t *testing.T) {
	rule := severityRule()
	rule.Enabled = false
	rules := &fakeAlertRules{rules: []*ca.AlertRule{rule}}
	dispatcher := &fakeDispatcher{}
	uc := newAlertEvaluator(rules, dispatcher, newFakeRepo())

	uc.EvaluateBatch(context.Background(), ref(), "ws-1", []*ca.Analysis{analysedAt("c-1", 100, now)})
	if len(dispatcher.all()) != 0 {
		t.Fatal("a disabled rule must not fire")
	}
}

// A rule belongs to one workspace. A batch of another workspace must not touch
// it, even for the same account id.
func TestAlertIsWorkspaceScoped(t *testing.T) {
	rules := &fakeAlertRules{rules: []*ca.AlertRule{severityRule()}}
	dispatcher := &fakeDispatcher{}
	uc := newAlertEvaluator(rules, dispatcher, newFakeRepo())

	uc.EvaluateBatch(context.Background(), ref(), "ws-2", []*ca.Analysis{analysedAt("c-1", 100, now)})
	if len(dispatcher.all()) != 0 {
		t.Fatal("another workspace's batch must not fire this rule")
	}
}

// THE concurrency property. Two replicas evaluating the same batch both decide
// the rule should fire; only the one that wins the claim sends.
func TestAlertOnlyTheClaimWinnerSends(t *testing.T) {
	rules := &fakeAlertRules{rules: []*ca.AlertRule{severityRule()}, claimLimit: 1}
	dispatcher := &fakeDispatcher{}
	uc := newAlertEvaluator(rules, dispatcher, newFakeRepo())

	batch := []*ca.Analysis{analysedAt("c-1", 92, now)}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			uc.EvaluateBatch(context.Background(), ref(), "ws-1", batch)
		}()
	}
	wg.Wait()

	if len(dispatcher.all()) != 1 {
		t.Fatalf("sent %d alerts from 4 concurrent evaluations, want exactly 1", len(dispatcher.all()))
	}
}

// A send that fails records why and does NOT release the claim: releasing it
// would retry on the next batch, seconds later, against a channel that is down.
func TestAlertFailureIsRecordedAndNotRetriedImmediately(t *testing.T) {
	rules := &fakeAlertRules{rules: []*ca.AlertRule{severityRule()}}
	dispatcher := &fakeDispatcher{err: errors.New("whatsapp offline")}
	uc := newAlertEvaluator(rules, dispatcher, newFakeRepo())

	uc.EvaluateBatch(context.Background(), ref(), "ws-1", []*ca.Analysis{analysedAt("c-1", 92, now)})

	if len(rules.failures) != 1 || !strings.Contains(rules.failures[0], "offline") {
		t.Fatalf("failures = %v, the reason must be stored for the operator", rules.failures)
	}
	if rules.claimed != 1 {
		t.Fatalf("claims = %d, the claim stands so the cooldown is the retry interval", rules.claimed)
	}
}

// Nothing about alerts may break a classification that was already paid for.
func TestAlertSurvivesAFailingRuleStore(t *testing.T) {
	rules := &fakeAlertRules{rules: []*ca.AlertRule{severityRule()}, listErr: errors.New("db down")}
	dispatcher := &fakeDispatcher{}
	uc := newAlertEvaluator(rules, dispatcher, newFakeRepo())

	uc.EvaluateBatch(context.Background(), ref(), "ws-1", []*ca.Analysis{analysedAt("c-1", 92, now)})
	if len(dispatcher.all()) != 0 {
		t.Fatal("nothing to send when the rules cannot be read")
	}
}

// A deployment with no channel bound evaluates nothing rather than panicking.
func TestAlertWithoutADispatcherDoesNothing(t *testing.T) {
	uc := NewAlertEvaluator(AlertDeps{Rules: &fakeAlertRules{rules: []*ca.AlertRule{severityRule()}}, Clock: fixedClock{now}})
	uc.EvaluateBatch(context.Background(), ref(), "ws-1", []*ca.Analysis{analysedAt("c-1", 100, now)})
}

// ---- windowed metrics ----

type statsRepo struct {
	*fakeRepo
	stats *ca.Stats
	calls int
	err   error
	seen  []ca.ListInput
}

func (s *statsRepo) GetStats(_ context.Context, in ca.ListInput) (*ca.Stats, error) {
	s.calls++
	s.seen = append(s.seen, in)
	if s.err != nil {
		return nil, s.err
	}
	copied := *s.stats
	return &copied, nil
}

func windowedRule(id string, metric ca.AlertMetric, threshold, windowMinutes int) *ca.AlertRule {
	r := &ca.AlertRule{
		ID: id, WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: ref().AccountID,
		Name: "Pico", Metric: metric, Threshold: threshold, WindowMinutes: windowMinutes,
		Channel: ca.AlertChannelUnofficial, Recipient: "5511999999999", Enabled: true,
	}
	r.Normalize()
	return r
}

func TestAlertFiresOnAWindowedCount(t *testing.T) {
	repo := &statsRepo{fakeRepo: newFakeRepo(), stats: &ca.Stats{Counters: ca.Counters{Analyzed: 40, StanceHostile: 14}}}
	rules := &fakeAlertRules{rules: []*ca.AlertRule{windowedRule("r", ca.AlertMetricHostileCount, 10, 60)}}
	dispatcher := &fakeDispatcher{}
	uc := newAlertEvaluator(rules, dispatcher, repo)

	uc.EvaluateBatch(context.Background(), ref(), "ws-1", []*ca.Analysis{analysedAt("c-1", 10, now)})

	sent := dispatcher.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d, want 1", len(sent))
	}
	if !strings.Contains(sent[0].Text, "14") {
		t.Fatalf("the measured value must be reported:\n%s", sent[0].Text)
	}
	// A windowed alert has no single comment behind it, so it must not quote
	// one as though that comment crossed the threshold.
	if strings.Contains(sent[0].Text, "ladrões") {
		t.Fatalf("a windowed alert must not quote a comment:\n%s", sent[0].Text)
	}
	// The window has to reach the query, or the count is over all time.
	if len(repo.seen) == 0 || repo.seen[0].From == nil {
		t.Fatal("the window must be sent to the stats query")
	}
	if got := now.Sub(*repo.seen[0].From); got != time.Hour {
		t.Fatalf("window = %s, want 1h", got)
	}
}

// Several rules watching the same span ask the database once.
func TestAlertSharesOneQueryPerWindow(t *testing.T) {
	repo := &statsRepo{fakeRepo: newFakeRepo(), stats: &ca.Stats{Counters: ca.Counters{Analyzed: 40, StanceHostile: 14, SeverityHighCount: 9}}}
	rules := &fakeAlertRules{rules: []*ca.AlertRule{
		windowedRule("r1", ca.AlertMetricHostileCount, 10, 60),
		windowedRule("r2", ca.AlertMetricHighSeverityCount, 5, 60),
		windowedRule("r3", ca.AlertMetricCommentVolume, 1000, 60),
	}}
	uc := newAlertEvaluator(rules, &fakeDispatcher{}, repo)

	uc.EvaluateBatch(context.Background(), ref(), "ws-1", []*ca.Analysis{analysedAt("c-1", 10, now)})
	if repo.calls != 1 {
		t.Fatalf("stats queries = %d, three rules on one window must ask once", repo.calls)
	}
}

// A quiet window has no score to have fallen. Reporting zero would fire every
// acceptance rule on a night with no comments at all.
func TestAlertAcceptanceScoreIgnoresAnEmptyWindow(t *testing.T) {
	repo := &statsRepo{fakeRepo: newFakeRepo(), stats: &ca.Stats{Counters: ca.Counters{Analyzed: 0}}}
	rules := &fakeAlertRules{rules: []*ca.AlertRule{windowedRule("r", ca.AlertMetricAcceptanceScore, 40, 60)}}
	dispatcher := &fakeDispatcher{}
	uc := newAlertEvaluator(rules, dispatcher, repo)

	uc.EvaluateBatch(context.Background(), ref(), "ws-1", []*ca.Analysis{analysedAt("c-1", 10, now)})
	if len(dispatcher.all()) != 0 {
		t.Fatal("an empty window is not a collapsed score")
	}
}

// A stats query that fails is "unknown", not zero: zero would fire an
// acceptance rule every time the database hiccupped.
func TestAlertWindowQueryFailureDoesNotFire(t *testing.T) {
	repo := &statsRepo{fakeRepo: newFakeRepo(), stats: &ca.Stats{}, err: errors.New("timeout")}
	rules := &fakeAlertRules{rules: []*ca.AlertRule{windowedRule("r", ca.AlertMetricAcceptanceScore, 40, 60)}}
	dispatcher := &fakeDispatcher{}
	uc := newAlertEvaluator(rules, dispatcher, repo)

	uc.EvaluateBatch(context.Background(), ref(), "ws-1", []*ca.Analysis{analysedAt("c-1", 10, now)})
	if len(dispatcher.all()) != 0 {
		t.Fatal("an unreadable window must not be treated as zero")
	}
}

func TestAlertAcceptanceScoreFiresOnACollapse(t *testing.T) {
	stats := &ca.Stats{Counters: ca.Counters{Analyzed: 30, StanceHostile: 25, StanceCritic: 5}}
	stats.Finalize()
	repo := &statsRepo{fakeRepo: newFakeRepo(), stats: stats}
	rules := &fakeAlertRules{rules: []*ca.AlertRule{windowedRule("r", ca.AlertMetricAcceptanceScore, 40, 60)}}
	dispatcher := &fakeDispatcher{}
	uc := newAlertEvaluator(rules, dispatcher, repo)

	uc.EvaluateBatch(context.Background(), ref(), "ws-1", []*ca.Analysis{analysedAt("c-1", 10, now)})
	if len(dispatcher.all()) != 1 {
		t.Fatalf("a score of %d under a threshold of 40 must fire", stats.AcceptanceScore)
	}
}

// Backfilled comments do not stop a WINDOWED rule from working: the window
// counts by the comment's own timestamp, so old rows fall outside it anyway,
// and the batch itself is only the trigger to look.
func TestAlertWindowedRuleStillEvaluatesOnAnOldBatch(t *testing.T) {
	repo := &statsRepo{fakeRepo: newFakeRepo(), stats: &ca.Stats{Counters: ca.Counters{Analyzed: 40, StanceHostile: 30}}}
	rules := &fakeAlertRules{rules: []*ca.AlertRule{windowedRule("r", ca.AlertMetricHostileCount, 10, 60)}}
	dispatcher := &fakeDispatcher{}
	uc := newAlertEvaluator(rules, dispatcher, repo)

	old := now.Add(-AlertFreshness - time.Hour)
	uc.EvaluateBatch(context.Background(), ref(), "ws-1", []*ca.Analysis{analysedAt("c-old", 100, old)})

	if len(dispatcher.all()) != 1 {
		t.Fatal("a windowed rule measures the window, not the batch")
	}
}

var _ = shared.Pagination{}
