package crmbulk_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/crmfilter"
)

type mockTargetResolver struct {
	calls   []TargetQuery
	refs    []EntryRef
	matched int64
	err     error
}

func (m *mockTargetResolver) ResolveTargets(_ context.Context, q TargetQuery) ([]EntryRef, int64, error) {
	m.calls = append(m.calls, q)
	if m.err != nil {
		return nil, 0, m.err
	}
	return m.refs, m.matched, nil
}

func stageFilter(stageIDs ...string) *crmfilter.Filter {
	return &crmfilter.Filter{Groups: []crmfilter.Group{{
		Conjunction: crmfilter.And,
		Predicates: []crmfilter.Predicate{{
			Field:    crmfilter.FieldStage,
			Operator: crmfilter.OpIn,
			Values:   stageIDs,
		}},
	}}}
}

func withResolver(r TargetResolver, sa *mockStageAssigner, bc *mockBroadcaster) *Service {
	svc := NewService(sa, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}, allowAll(), bc)
	svc.SetTargetResolver(r)
	return svc
}

func TestBulkApply_FilterExpandsToTargetsAndFansOut(t *testing.T) {
	res := &mockTargetResolver{refs: targets("e1", "e2", "e3"), matched: 3}
	sa := &mockStageAssigner{}
	bc := &mockBroadcaster{}
	svc := withResolver(res, sa, bc)

	out := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "actor-1", Action: ActionMoveStage,
		Value: "stage-b", Filter: stageFilter("stage-a"),
	})

	if out.Succeeded != 3 || len(out.Failed) != 0 || out.Forbidden {
		t.Fatalf("expected 3/0/notForbidden, got %+v", out)
	}
	if len(sa.calls) != 3 {
		t.Fatalf("expected the filter to fan out to 3 stage moves, got %d", len(sa.calls))
	}
	for _, c := range sa.calls {
		if c.StageID != "stage-b" {
			t.Errorf("target stage not forwarded: %q", c.StageID)
		}
	}
	if len(bc.stage) != 3 {
		t.Errorf("every filter-addressed success must broadcast, got %v", bc.stage)
	}
}

func TestBulkApply_FilterForwardsActorScopeToResolver(t *testing.T) {
	res := &mockTargetResolver{refs: targets("e1"), matched: 1}
	svc := withResolver(res, &mockStageAssigner{}, &mockBroadcaster{})

	f := stageFilter("stage-a")
	svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-9", ActorID: "actor-7", IsAdmin: true,
		SelectedDepartmentID: "dept-3", Action: ActionMoveStage, Value: "stage-b", Filter: f,
	})

	if len(res.calls) != 1 {
		t.Fatalf("expected one resolve call, got %d", len(res.calls))
	}
	got := res.calls[0]
	if got.WorkspaceID != "ws-9" || got.ActorID != "actor-7" || !got.IsAdmin ||
		got.SelectedDepartmentID != "dept-3" {
		t.Errorf("actor scope not forwarded to the resolver: %+v", got)
	}
	if got.Limit != MaxFilterTargets {
		t.Errorf("resolver must be capped at MaxFilterTargets, got %d", got.Limit)
	}
	if len(got.Filter.Groups) != 1 || got.Filter.Groups[0].Predicates[0].Field != crmfilter.FieldStage {
		t.Errorf("filter not forwarded intact: %+v", got.Filter)
	}
}

func TestBulkApply_ExplicitTargetsWinOverFilter(t *testing.T) {
	res := &mockTargetResolver{refs: targets("x1", "x2", "x3"), matched: 3}
	sa := &mockStageAssigner{}
	svc := withResolver(res, sa, &mockBroadcaster{})

	out := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "a", Action: ActionMoveStage, Value: "stage-b",
		Targets: targets("e1"), Filter: stageFilter("stage-a"),
	})

	if len(res.calls) != 0 {
		t.Fatal("a hand-picked target list must not trigger a filter expansion")
	}
	if out.Succeeded != 1 || len(sa.calls) != 1 || sa.calls[0].EntryID != "e1" {
		t.Fatalf("expected only the explicit target to be touched, got %+v / %+v", out, sa.calls)
	}
}

func TestBulkApply_EmptyFilterIsStillATargetingRequest(t *testing.T) {
	res := &mockTargetResolver{refs: targets("e1", "e2"), matched: 2}
	svc := withResolver(res, &mockStageAssigner{}, &mockBroadcaster{})

	out := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "a", Action: ActionMoveStage, Value: "stage-b",
		Filter: &crmfilter.Filter{},
	})

	if len(res.calls) != 1 {
		t.Fatalf("an empty filter must still resolve, got %d calls", len(res.calls))
	}
	if out.Succeeded != 2 {
		t.Fatalf("expected 2 succeeded, got %+v", out)
	}
}

func TestBulkApply_ReportsTruncationWhenTheCapBinds(t *testing.T) {
	res := &mockTargetResolver{refs: targets("e1", "e2"), matched: 5300}
	svc := withResolver(res, &mockStageAssigner{}, &mockBroadcaster{})

	out := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "a", Action: ActionMoveStage,
		Value: "stage-b", Filter: stageFilter("stage-a"),
	})

	if !out.Truncated {
		t.Error("a match larger than the returned page must set Truncated")
	}
	if out.Matched != 5300 {
		t.Errorf("Matched must carry the true total, got %d", out.Matched)
	}
	if out.Succeeded != 2 {
		t.Errorf("only the returned refs are applied, got %d", out.Succeeded)
	}
}

func TestBulkApply_NoTruncationWhenEverythingFits(t *testing.T) {
	res := &mockTargetResolver{refs: targets("e1", "e2"), matched: 2}
	svc := withResolver(res, &mockStageAssigner{}, &mockBroadcaster{})

	out := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "a", Action: ActionMoveStage,
		Value: "stage-b", Filter: stageFilter("stage-a"),
	})

	if out.Truncated {
		t.Errorf("nothing was dropped; Truncated must stay false: %+v", out)
	}
}

func TestBulkApply_RBACGateRunsBeforeAnyResolve(t *testing.T) {
	res := &mockTargetResolver{refs: targets("e1"), matched: 1}
	svc := NewService(&mockStageAssigner{}, &mockLabelAssigner{}, &mockLabelRemover{},
		&mockEntryAssigner{}, &mockAuthorizer{}, &mockBroadcaster{})
	svc.SetTargetResolver(res)

	out := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "a", Action: ActionMoveStage,
		Value: "stage-b", Filter: stageFilter("stage-a"),
	})

	if !out.Forbidden {
		t.Fatal("expected a hard RBAC denial")
	}
	if len(res.calls) != 0 {
		t.Error("a forbidden actor must never reach the resolver")
	}
}

func TestBulkApply_PerEntryScopeStillAppliesToResolvedTargets(t *testing.T) {
	res := &mockTargetResolver{refs: targets("e1", "e2"), matched: 2}
	authz := allowAll()
	authz.denyEntry = map[string]bool{"e2": true}
	sa := &mockStageAssigner{}
	svc := NewService(sa, &mockLabelAssigner{}, &mockLabelRemover{},
		&mockEntryAssigner{}, authz, &mockBroadcaster{})
	svc.SetTargetResolver(res)

	out := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "a", Action: ActionMoveStage,
		Value: "stage-b", Filter: stageFilter("stage-a"),
	})

	if out.Succeeded != 1 || len(out.Failed) != 1 {
		t.Fatalf("out-of-scope resolved entries must fail, not apply: %+v", out)
	}
	if out.Failed[0].EntryID != "e2" {
		t.Errorf("wrong entry rejected: %+v", out.Failed)
	}
}

func TestBulkApply_ResolverErrorIsReportedNotSilentlyEmpty(t *testing.T) {
	res := &mockTargetResolver{err: errors.New("db down")}
	sa := &mockStageAssigner{}
	svc := withResolver(res, sa, &mockBroadcaster{})

	out := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "a", Action: ActionMoveStage,
		Value: "stage-b", Filter: stageFilter("stage-a"),
	})

	if out.Succeeded != 0 || len(sa.calls) != 0 {
		t.Fatal("a failed resolve must touch nothing")
	}
	if len(out.Failed) != 1 {
		t.Fatalf("a failed resolve must be reported, got %+v", out)
	}
}

func TestBulkApply_FilterWithoutAResolverFailsClosed(t *testing.T) {
	sa := &mockStageAssigner{}
	svc := NewService(sa, &mockLabelAssigner{}, &mockLabelRemover{},
		&mockEntryAssigner{}, allowAll(), &mockBroadcaster{})

	out := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "a", Action: ActionMoveStage,
		Value: "stage-b", Filter: stageFilter("stage-a"),
	})

	if len(sa.calls) != 0 || out.Succeeded != 0 {
		t.Fatal("an unresolvable filter must touch nothing")
	}
	if len(out.Failed) != 1 || out.Failed[0].Error != ErrTargetResolverUnavailable.Error() {
		t.Fatalf("expected ErrTargetResolverUnavailable, got %+v", out.Failed)
	}
}

func TestBulkApply_NoFilterDoesNotReachTheResolver(t *testing.T) {
	res := &mockTargetResolver{refs: targets("e1"), matched: 1}
	svc := withResolver(res, &mockStageAssigner{}, &mockBroadcaster{})

	out := svc.BulkApply(context.Background(), BulkInput{
		WorkspaceID: "ws-1", ActorID: "a", Action: ActionMoveStage, Value: "stage-b",
	})

	if len(res.calls) != 0 {
		t.Error("no filter means no read at all")
	}
	if out.Succeeded != 0 || len(out.Failed) != 0 || out.Forbidden {
		t.Fatalf("expected an empty non-forbidden result, got %+v", out)
	}
}
