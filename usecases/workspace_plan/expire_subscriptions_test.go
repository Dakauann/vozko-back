package workspace_plan_usecase

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type countingExpireRepo struct {
	calls         []expireCall
	returns       []expireReturn
	callIndex     int
	subscriptions map[string]*workspace_plan.WorkspaceSubscription
}

type expireCall struct {
	at        time.Time
	batchSize int
}

type expireReturn struct {
	n     int64
	wsIDs []string
	err   error
}

func (r *countingExpireRepo) ExpireOverdue(at time.Time, batchSize int) ([]string, error) {
	r.calls = append(r.calls, expireCall{at: at, batchSize: batchSize})
	idx := r.callIndex
	if idx >= len(r.returns) {
		idx = len(r.returns) - 1
	}
	r.callIndex++
	ret := r.returns[idx]
	if ret.err != nil {
		return nil, ret.err
	}
	if ret.wsIDs != nil {
		return ret.wsIDs, nil
	}
	ids := make([]string, ret.n)
	for i := range ids {
		ids[i] = fmt.Sprintf("ws-%d", i)
	}
	return ids, nil
}

func (r *countingExpireRepo) Create(_ *workspace_plan.WorkspaceSubscription) error { return nil }
func (r *countingExpireRepo) Update(_ *workspace_plan.WorkspaceSubscription) error { return nil }
func (r *countingExpireRepo) GetCurrentByWorkspaceID(_ string, _ time.Time) (*workspace_plan.WorkspaceSubscription, error) {
	return nil, workspace_plan.ErrSubscriptionNotCurrent
}
func (r *countingExpireRepo) GetLatestByWorkspaceID(_ string) (*workspace_plan.WorkspaceSubscription, error) {
	return nil, workspace_plan.ErrSubscriptionNotFound
}
func (r *countingExpireRepo) GetCurrentByWorkspaceIDs(_ []string, _ time.Time) (map[string]*workspace_plan.WorkspaceSubscription, error) {
	return nil, nil
}
func (r *countingExpireRepo) ListUpcomingExpirations(_, _ time.Time, _ int) ([]*workspace_plan.WorkspaceSubscription, error) {
	return nil, nil
}
func (r *countingExpireRepo) ListActiveBillingDue(time.Time, string, int) ([]*workspace_plan.WorkspaceSubscription, error) {
	return nil, nil
}

func TestExpireSubscriptions_NoOverdue(t *testing.T) {
	repo := &countingExpireRepo{returns: []expireReturn{{n: 0, err: nil}}}
	fixedNow := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	uc := &expireSubscriptionsUseCase{
		subscriptions: repo,
		batchSize:     500,
		now:           func() time.Time { return fixedNow },
	}

	total, err := uc.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected 0, got %d", total)
	}
	if len(repo.calls) != 1 {
		t.Fatalf("expected exactly 1 call, got %d", len(repo.calls))
	}
	if repo.calls[0].batchSize != 500 {
		t.Fatalf("expected batchSize 500, got %d", repo.calls[0].batchSize)
	}
	if !repo.calls[0].at.Equal(fixedNow) {
		t.Fatalf("expected time %v, got %v", fixedNow, repo.calls[0].at)
	}
}

func TestExpireSubscriptions_SingleBatch(t *testing.T) {

	repo := &countingExpireRepo{returns: []expireReturn{{n: 42, err: nil}}}
	fixedNow := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	uc := &expireSubscriptionsUseCase{
		subscriptions: repo,
		batchSize:     500,
		now:           func() time.Time { return fixedNow },
	}

	total, err := uc.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 42 {
		t.Fatalf("expected 42, got %d", total)
	}
	if len(repo.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(repo.calls))
	}
}

func TestExpireSubscriptions_MultipleBatches(t *testing.T) {

	repo := &countingExpireRepo{returns: []expireReturn{
		{n: 500, err: nil},
		{n: 200, err: nil},
	}}
	fixedNow := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	uc := &expireSubscriptionsUseCase{
		subscriptions: repo,
		batchSize:     500,
		now:           func() time.Time { return fixedNow },
	}

	total, err := uc.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 700 {
		t.Fatalf("expected 700, got %d", total)
	}
	if len(repo.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(repo.calls))
	}

	for i, c := range repo.calls {
		if !c.at.Equal(fixedNow) {
			t.Fatalf("call %d: expected time %v, got %v", i, fixedNow, c.at)
		}
	}
}

func TestExpireSubscriptions_ExactBatchThenZero(t *testing.T) {

	repo := &countingExpireRepo{returns: []expireReturn{
		{n: 100, err: nil},
		{n: 0, err: nil},
	}}
	uc := &expireSubscriptionsUseCase{
		subscriptions: repo,
		batchSize:     100,
		now:           func() time.Time { return time.Now().UTC() },
	}

	total, err := uc.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 100 {
		t.Fatalf("expected 100, got %d", total)
	}
	if len(repo.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(repo.calls))
	}
}

func TestExpireSubscriptions_ErrorOnFirstBatch(t *testing.T) {
	dbErr := errors.New("database connection lost")
	repo := &countingExpireRepo{returns: []expireReturn{{n: 0, err: dbErr}}}
	uc := &expireSubscriptionsUseCase{
		subscriptions: repo,
		batchSize:     500,
		now:           func() time.Time { return time.Now().UTC() },
	}

	total, err := uc.Execute()
	if !errors.Is(err, dbErr) {
		t.Fatalf("expected dbErr, got %v", err)
	}
	if total != 0 {
		t.Fatalf("expected 0 on error, got %d", total)
	}
}

func TestExpireSubscriptions_ErrorOnSecondBatch(t *testing.T) {

	dbErr := errors.New("timeout")
	repo := &countingExpireRepo{returns: []expireReturn{
		{n: 500, err: nil},
		{n: 0, err: dbErr},
	}}
	uc := &expireSubscriptionsUseCase{
		subscriptions: repo,
		batchSize:     500,
		now:           func() time.Time { return time.Now().UTC() },
	}

	total, err := uc.Execute()
	if !errors.Is(err, dbErr) {
		t.Fatalf("expected dbErr, got %v", err)
	}
	if total != 500 {
		t.Fatalf("expected partial total 500, got %d", total)
	}
}

func TestExpireSubscriptions_SmallBatchSize(t *testing.T) {

	repo := &countingExpireRepo{returns: []expireReturn{
		{n: 2, err: nil},
		{n: 2, err: nil},
		{n: 1, err: nil},
	}}
	uc := &expireSubscriptionsUseCase{
		subscriptions: repo,
		batchSize:     2,
		now:           func() time.Time { return time.Now().UTC() },
	}

	total, err := uc.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 5 {
		t.Fatalf("expected 5, got %d", total)
	}
	if len(repo.calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(repo.calls))
	}
}

// TestExpireSubscriptions_DunningGrace: the production constructor must expire only subscriptions
// overdue BEYOND the dunning grace, so a monthly sub unpaid at its anchor keeps serving through the
// dunning window (the day-27 sweep owns the cancellation). The cutoff passed to the repo is now minus
// the grace, never now itself.
func TestExpireSubscriptions_DunningGrace(t *testing.T) {
	repo := &countingExpireRepo{returns: []expireReturn{{n: 0, err: nil}}}
	fixedNow := time.Date(2026, 2, 24, 12, 0, 0, 0, time.UTC) // the day after the anchor, mid-dunning
	uc := NewExpireSubscriptionsUseCase(repo, nil).(*expireSubscriptionsUseCase)
	uc.now = func() time.Time { return fixedNow }

	if _, err := uc.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(repo.calls) != 1 {
		t.Fatalf("expected one ExpireOverdue call, got %d", len(repo.calls))
	}
	want := fixedNow.AddDate(0, 0, -expireDunningGraceDays)
	if !repo.calls[0].at.Equal(want) {
		t.Fatalf("ExpireOverdue cutoff = %s, want now minus the dunning grace %s (mid-dunning subs must NOT be expired)", repo.calls[0].at, want)
	}
	// A sub whose period ended on the anchor (the 23rd) must still be inside the grace, so not yet expired.
	anchorEnd := time.Date(2026, 2, 23, 12, 0, 0, 0, time.UTC)
	if !anchorEnd.After(repo.calls[0].at) {
		t.Fatalf("a sub ending on the anchor (%s) must be after the cutoff (%s), i.e. still protected by dunning", anchorEnd, repo.calls[0].at)
	}
}

func TestNewExpireSubscriptionsUseCase_Constructor(t *testing.T) {
	repo := &countingExpireRepo{returns: []expireReturn{{n: 0, err: nil}}}
	uc := NewExpireSubscriptionsUseCase(repo, nil)
	if uc == nil {
		t.Fatal("expected non-nil usecase")
	}

	total, err := uc.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected 0, got %d", total)
	}
}

type recordingReducer struct{ reduced []string }

func (r *recordingReducer) OnEntitlementReduced(workspaceID string, kind workspace_addon.EntitlementKind) error {
	r.reduced = append(r.reduced, workspaceID+":"+string(kind))
	return nil
}
func (r *recordingReducer) OnEntitlementIncreased(string, workspace_addon.EntitlementKind) error {
	return nil
}

func TestExpireSubscriptions_SuspendsWhatsAppForEachExpiredWorkspace(t *testing.T) {
	// wsA appears twice (e.g. an active + a cancelled row); it must reconcile once.
	repo := &countingExpireRepo{returns: []expireReturn{{wsIDs: []string{"wsA", "wsB", "wsA"}}}}
	reducer := &recordingReducer{}
	uc := &expireSubscriptionsUseCase{
		subscriptions: repo,
		onReduced:     reducer,
		batchSize:     500,
		now:           func() time.Time { return time.Now().UTC() },
	}

	total, err := uc.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected 3 expired rows, got %d", total)
	}
	if len(reducer.reduced) != 2 {
		t.Fatalf("expected 2 distinct workspace reconciles, got %v", reducer.reduced)
	}
	for _, r := range reducer.reduced {
		if !strings.HasSuffix(r, ":"+string(workspace_addon.EntitlementWhatsAppBusinessPhones)) {
			t.Fatalf("plan lapse must reconcile the whatsapp entitlement, got %s", r)
		}
	}
	seen := map[string]bool{}
	for _, r := range reducer.reduced {
		seen[strings.Split(r, ":")[0]] = true
	}
	if !seen["wsA"] || !seen["wsB"] {
		t.Fatalf("expected both wsA and wsB reconciled, got %v", reducer.reduced)
	}
}

func TestExpireSubscriptions_NoReducerConfigured_DoesNotPanic(t *testing.T) {
	repo := &countingExpireRepo{returns: []expireReturn{{n: 3, err: nil}}}
	uc := &expireSubscriptionsUseCase{
		subscriptions: repo,
		batchSize:     500,
		now:           func() time.Time { return time.Now().UTC() },
	}
	total, err := uc.Execute()
	if err != nil || total != 3 {
		t.Fatalf("expected total 3 with no error, got total=%d err=%v", total, err)
	}
}

func TestExpireSubscriptions_TimestampConsistency(t *testing.T) {

	fixedNow := time.Date(2026, 6, 15, 8, 30, 0, 0, time.UTC)
	repo := &countingExpireRepo{returns: []expireReturn{
		{n: 10, err: nil},
		{n: 10, err: nil},
		{n: 3, err: nil},
	}}
	uc := &expireSubscriptionsUseCase{
		subscriptions: repo,
		batchSize:     10,
		now:           func() time.Time { return fixedNow },
	}

	_, err := uc.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, c := range repo.calls {
		if !c.at.Equal(fixedNow) {
			t.Errorf("call %d used time %v, expected %v", i, c.at, fixedNow)
		}
	}
}
