package crmbulk_usecase

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"testing"

	"vozko/domain/cache"
	"vozko/domain/crmfilter"
	"vozko/domain/selection"
)

type resolveCall struct {
	scope selection.Scope
	sel   selection.Selection
	after string
	limit int
}

type pagedResolver struct {
	ids        []string
	calls      []resolveCall
	countCalls []selection.Selection
	err        error
}

func idsNamed(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("e%05d", i)
	}
	sort.Strings(out)
	return out
}

func (p *pagedResolver) remaining(s selection.Selection) []string {
	out := make([]string, 0, len(p.ids))
	for _, id := range p.ids {
		if !slices.Contains(s.ExcludeIDs, id) {
			out = append(out, id)
		}
	}
	return out
}

func (p *pagedResolver) Count(_ context.Context, _ selection.Scope, s selection.Selection) (int, error) {
	p.countCalls = append(p.countCalls, s)
	return len(p.remaining(s)), p.err
}

func (p *pagedResolver) Resolve(_ context.Context, scope selection.Scope, s selection.Selection, after string, limit int) ([]selection.Ref, error) {
	p.calls = append(p.calls, resolveCall{scope: scope, sel: s, after: after, limit: limit})
	if p.err != nil {
		return nil, p.err
	}
	ids := p.remaining(s)
	start := sort.SearchStrings(ids, after)
	if start < len(ids) && ids[start] == after {
		start++
	}
	end := min(start+limit, len(ids))
	page := make([]selection.Ref, 0, end-start)
	for _, id := range ids[start:end] {
		page = append(page, selection.Ref{ID: id, Type: "whatsapp"})
	}
	return page, nil
}

type openGate struct{ acquired *int }

func (g openGate) Acquire(context.Context) (func(), error) {
	if g.acquired != nil {
		*g.acquired++
	}
	return func() {}, nil
}

type busyGate struct{}

func (busyGate) Acquire(context.Context) (func(), error) { return nil, cache.ErrGateBusy }

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

func allMatching(f *crmfilter.Filter) selection.Selection {
	return selection.Selection{Mode: selection.ModeAllMatching, Filter: f, Fingerprint: selection.Fingerprint(*f)}
}

func withResolver(r selection.Resolver, sa *mockStageAssigner, bc *mockBroadcaster) *Service {
	svc := NewService(sa, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}, allowAll(), bc)
	svc.SetSelection(r, openGate{})
	return svc
}

func filtered(sel selection.Selection) BulkInput {
	return BulkInput{WorkspaceID: "ws-1", ActorID: "actor-1", Action: ActionMoveStage, Value: "stage-b", Selection: sel}
}

func TestBulkApply_FilterResolvesTheWholeSetInOneGatedRead(t *testing.T) {
	res := &pagedResolver{ids: idsNamed(1234)}
	sa := &mockStageAssigner{}
	bc := &mockBroadcaster{}
	acquired := 0
	svc := NewService(sa, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}, allowAll(), bc)
	svc.SetSelection(res, openGate{acquired: &acquired})

	out := run(svc, filtered(allMatching(stageFilter("stage-a"))))

	if out.err != nil || out.Succeeded != 1234 || out.Matched != 1234 || out.Eligible != 1234 || out.Truncated {
		t.Fatalf("expected every match applied, got %+v", out)
	}
	if len(res.countCalls) != 1 || len(res.calls) != 1 || acquired != 1 {
		t.Fatalf("expected one count and one read under one gate slot, got %d counts, %d reads, %d slots", len(res.countCalls), len(res.calls), acquired)
	}
	if read := res.calls[0]; read.after != "" || read.limit != MaxFilterTargets+1 {
		t.Fatalf("the set is read once from the start with one row past the cap, got after=%q limit=%d", read.after, read.limit)
	}
	seen := map[string]bool{}
	for _, c := range sa.calls {
		if seen[c.EntryID] {
			t.Fatalf("entry %s applied twice", c.EntryID)
		}
		seen[c.EntryID] = true
	}
	if len(bc.stage) != 1234 {
		t.Errorf("every success must broadcast, got %d", len(bc.stage))
	}
}

func TestBulkApply_FilterStopsAtTheCapAndReportsTruncation(t *testing.T) {
	res := &pagedResolver{ids: idsNamed(MaxFilterTargets + 300)}
	sa := &mockStageAssigner{}
	acquired := 0
	svc := NewService(sa, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}, allowAll(), &mockBroadcaster{})
	svc.SetSelection(res, openGate{acquired: &acquired})

	out := run(svc, filtered(allMatching(stageFilter("stage-a"))))

	if out.err != nil || !out.Truncated || out.Matched != MaxFilterTargets+300 || out.Succeeded != MaxFilterTargets {
		t.Fatalf("expected a truncated run at the cap, got matched=%d succeeded=%d truncated=%v err=%v",
			out.Matched, out.Succeeded, out.Truncated, out.err)
	}
	if len(sa.calls) != MaxFilterTargets {
		t.Fatalf("applied %d, want %d", len(sa.calls), MaxFilterTargets)
	}
	if len(res.countCalls) != 1 || len(res.calls) != 1 || acquired != 1 {
		t.Fatalf("a set above the cap is still one count and one read under one gate slot, got %d counts, %d reads, %d slots", len(res.countCalls), len(res.calls), acquired)
	}
}

func TestBulkApply_TheBulkReadFitsTheResolvePageCap(t *testing.T) {
	if err := selection.ValidatePage(MaxFilterTargets + 1); err != nil {
		t.Fatalf("the cap plus the truncation row must be one valid read: %v", err)
	}
}

func TestBulkApply_AResolverThatOverreadsIsTrimmedToTheCap(t *testing.T) {
	res := &overreadingResolver{pagedResolver{ids: idsNamed(MaxFilterTargets + 50)}}
	sa := &mockStageAssigner{}
	svc := withResolver(res, sa, &mockBroadcaster{})

	out := run(svc, filtered(allMatching(stageFilter("stage-a"))))

	if out.err != nil || !out.Truncated || len(sa.calls) != MaxFilterTargets {
		t.Fatalf("an over-long read must be cut at the cap and flagged, got applied=%d %+v %v", len(sa.calls), out.BulkResult, out.err)
	}
}

type overreadingResolver struct{ pagedResolver }

func (o *overreadingResolver) Resolve(ctx context.Context, scope selection.Scope, s selection.Selection, after string, _ int) ([]selection.Ref, error) {
	return o.pagedResolver.Resolve(ctx, scope, s, after, len(o.ids))
}

func TestBulkApply_ExclusionsThatBringTheSetToTheCapAreNotTruncated(t *testing.T) {
	res := &pagedResolver{ids: idsNamed(MaxFilterTargets + 2)}
	sa := &mockStageAssigner{}
	svc := withResolver(res, sa, &mockBroadcaster{})
	sel := allMatching(stageFilter("stage-a"))
	sel.ExcludeIDs = []string{res.ids[0], res.ids[1]}

	out := run(svc, filtered(sel))

	if out.err != nil || out.Truncated || out.Succeeded != MaxFilterTargets || out.Matched != MaxFilterTargets+2 {
		t.Fatalf("every remaining entry was applied, nothing is left: %+v %v", out.BulkResult, out.err)
	}
}

func TestBulkApply_TheCountedSelectionIsConfirmedBeforeExclusions(t *testing.T) {
	res := &pagedResolver{ids: idsNamed(340)}
	sa := &mockStageAssigner{}
	svc := withResolver(res, sa, &mockBroadcaster{})
	scope := selection.Scope{WorkspaceID: "ws-1", ActorID: "actor-1"}

	cases := []struct {
		name   string
		filter crmfilter.Filter
	}{
		{"all matching minus a few", *stageFilter("stage-a")},
		{"everyone minus a few", crmfilter.Filter{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sa.calls = nil
			matched, fingerprint, err := svc.Count(context.Background(), scope, tc.filter)
			if err != nil || matched != 340 {
				t.Fatalf("count = %d, %v", matched, err)
			}
			sel := selection.ForFilter(tc.filter)
			sel.ExpectedCount, sel.Fingerprint = matched, fingerprint
			sel.ExcludeIDs = []string{res.ids[3], res.ids[200], "outside-the-scope"}

			out := run(svc, filtered(sel))

			if out.err != nil || out.Matched != 340 || out.Eligible != 338 || out.Succeeded != 338 {
				t.Fatalf("count then exclusions must apply to the rest: %+v %v", out.BulkResult, out.err)
			}
			for _, c := range sa.calls {
				if c.EntryID == res.ids[3] || c.EntryID == res.ids[200] {
					t.Fatalf("excluded entry %s was applied", c.EntryID)
				}
			}
			if last := res.countCalls[len(res.countCalls)-1]; last.ExcludeIDs != nil {
				t.Fatalf("the confirming count must ignore exclusions, got %v", last.ExcludeIDs)
			}
		})
	}
}

func TestBulkApply_FilterForwardsActorScopeToResolver(t *testing.T) {
	res := &pagedResolver{ids: idsNamed(1)}
	svc := withResolver(res, &mockStageAssigner{}, &mockBroadcaster{})

	in := filtered(allMatching(stageFilter("stage-a")))
	in.WorkspaceID, in.ActorID, in.IsAdmin, in.SelectedDepartmentID = "ws-9", "actor-7", true, "dept-3"
	in.Selection.ExcludeIDs = []string{"e9"}
	run(svc, in)

	if len(res.calls) != 1 {
		t.Fatalf("expected one resolve call, got %d", len(res.calls))
	}
	got := res.calls[0]
	if got.scope != (selection.Scope{WorkspaceID: "ws-9", ActorID: "actor-7", DepartmentID: "dept-3", IsAdmin: true}) {
		t.Errorf("actor scope not forwarded to the resolver: %+v", got.scope)
	}
	if got.sel.Filter == nil || len(got.sel.ExcludeIDs) != 1 {
		t.Errorf("selection not forwarded intact: %+v", got.sel)
	}
}

func TestBulkApply_SelectionRulesRunBeforeAnyRead(t *testing.T) {
	stale := allMatching(stageFilter("stage-a"))
	stale.Fingerprint = selection.Fingerprint(*stageFilter("stage-z"))
	everyone := selection.Selection{Mode: selection.ModeEveryone, ExpectedCount: 3, Fingerprint: selection.Fingerprint(crmfilter.Filter{})}

	cases := []struct {
		name    string
		sel     selection.Selection
		targets []EntryRef
		want    error
	}{
		{"empty filter is not all matching", selection.Selection{Mode: selection.ModeAllMatching, Filter: &crmfilter.Filter{}, Fingerprint: selection.Fingerprint(crmfilter.Filter{})}, nil, selection.ErrFilterRequired},
		{"everyone needs the confirmed count", selection.Selection{Mode: selection.ModeEveryone, Fingerprint: selection.Fingerprint(crmfilter.Filter{})}, nil, selection.ErrEveryoneUnconfirmed},
		{"count taken on another filter", stale, nil, selection.ErrFingerprintMismatch},
		{"picks and a filter at once", selection.Selection{Mode: selection.ModeIDs, Filter: stageFilter("stage-a")}, targets("e1"), selection.ErrAmbiguousSelection},
		{"all matching with picks", allMatching(stageFilter("stage-a")), targets("e1"), selection.ErrAmbiguousSelection},
		{"everyone with picks", everyone, targets("e1"), selection.ErrAmbiguousSelection},
		{"a legacy filter without the counted fingerprint", selection.Selection{Filter: stageFilter("stage-a")}, nil, selection.ErrFingerprintMismatch},
		{"a legacy empty filter without the count", selection.Selection{Filter: &crmfilter.Filter{}}, nil, selection.ErrEveryoneUnconfirmed},
		{"nothing picked and no filter", selection.Selection{}, nil, selection.ErrUnknownMode},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := &pagedResolver{ids: idsNamed(3)}
			sa := &mockStageAssigner{}
			svc := withResolver(res, sa, &mockBroadcaster{})
			in := filtered(tc.sel)
			in.Targets = tc.targets

			out := run(svc, in)

			if !errors.Is(out.err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, out.err)
			}
			if len(res.calls)+len(res.countCalls) != 0 || len(sa.calls) != 0 {
				t.Fatal("a refused selection must read and touch nothing")
			}
		})
	}
}

func TestBulkApply_LegacyPicksWithoutAModeAreIDs(t *testing.T) {
	res := &pagedResolver{ids: idsNamed(3)}
	sa := &mockStageAssigner{}
	svc := withResolver(res, sa, &mockBroadcaster{})
	in := filtered(selection.Selection{})
	in.Targets = targets("e1", "e2")

	out := run(svc, in)

	if out.err != nil || out.Succeeded != 2 || len(res.calls) != 0 {
		t.Fatalf("picks without a mode are applied as ids, got %+v %v", out.BulkResult, out.err)
	}
}

func TestBulkApply_FirstNIsNotOfferedForConversations(t *testing.T) {
	res := &pagedResolver{ids: idsNamed(3)}
	svc := withResolver(res, &mockStageAssigner{}, &mockBroadcaster{})
	sel := allMatching(stageFilter("stage-a"))
	sel.Mode, sel.Limit = selection.ModeFirstN, 2

	if out := run(svc, filtered(sel)); !errors.Is(out.err, selection.ErrModeUnsupported) || len(res.calls)+len(res.countCalls) != 0 {
		t.Fatalf("want ErrModeUnsupported before any read, got %v", out.err)
	}
}

func TestBulkApply_EveryoneConfirmedByTheCountReachesTheWholeScope(t *testing.T) {
	res := &pagedResolver{ids: idsNamed(4)}
	sa := &mockStageAssigner{}
	svc := withResolver(res, sa, &mockBroadcaster{})
	everyone := selection.Selection{Mode: selection.ModeEveryone, ExpectedCount: 4, Fingerprint: selection.Fingerprint(crmfilter.Filter{})}

	out := run(svc, filtered(everyone))

	if out.err != nil || out.Succeeded != 4 || len(sa.calls) != 4 {
		t.Fatalf("expected 4 applied, got %+v", out)
	}
}

func TestBulkApply_ChangedCountIsRefusedBeforeAnyPage(t *testing.T) {
	res := &pagedResolver{ids: idsNamed(5)}
	sa := &mockStageAssigner{}
	svc := withResolver(res, sa, &mockBroadcaster{})
	sel := allMatching(stageFilter("stage-a"))
	sel.ExpectedCount = 4

	out := run(svc, filtered(sel))

	var changed *selection.CountChangedError
	if !errors.As(out.err, &changed) || changed.Matched != 5 || changed.Expected != 4 {
		t.Fatalf("want a count change 4 -> 5, got %v", out.err)
	}
	if len(sa.calls) != 0 || len(res.calls) != 0 {
		t.Fatal("a changed selection must resolve and touch nothing")
	}
}

func TestBulkApply_ExplicitTargetsNeverReachTheResolver(t *testing.T) {
	res := &pagedResolver{ids: idsNamed(3)}
	sa := &mockStageAssigner{}
	svc := withResolver(res, sa, &mockBroadcaster{})

	out := run(svc, BulkInput{
		WorkspaceID: "ws-1", ActorID: "a", Action: ActionMoveStage, Value: "stage-b",
		Selection: byIDs, Targets: targets("e1"),
	})

	if len(res.calls)+len(res.countCalls) != 0 {
		t.Fatal("a hand-picked target list must not trigger a filter expansion")
	}
	if out.Succeeded != 1 || out.Matched != 1 || len(sa.calls) != 1 || sa.calls[0].EntryID != "e1" {
		t.Fatalf("expected only the explicit target to be touched, got %+v / %+v", out, sa.calls)
	}
}

func TestBulkApply_RBACGateRunsBeforeAnyResolve(t *testing.T) {
	res := &pagedResolver{ids: idsNamed(1)}
	svc := NewService(&mockStageAssigner{}, &mockLabelAssigner{}, &mockLabelRemover{},
		&mockEntryAssigner{}, &mockAuthorizer{}, &mockBroadcaster{})
	svc.SetSelection(res, openGate{})

	out := run(svc, filtered(allMatching(stageFilter("stage-a"))))

	if !out.Forbidden {
		t.Fatal("expected a hard RBAC denial")
	}
	if len(res.calls)+len(res.countCalls) != 0 {
		t.Error("a forbidden actor must never reach the resolver")
	}
}

func TestBulkApply_PerEntryScopeStillAppliesToResolvedTargets(t *testing.T) {
	res := &pagedResolver{ids: []string{"e1", "e2"}}
	authz := allowAll()
	authz.denyEntry = map[string]bool{"e2": true}
	sa := &mockStageAssigner{}
	svc := NewService(sa, &mockLabelAssigner{}, &mockLabelRemover{},
		&mockEntryAssigner{}, authz, &mockBroadcaster{})
	svc.SetSelection(res, openGate{})

	out := run(svc, filtered(allMatching(stageFilter("stage-a"))))

	if out.Succeeded != 1 || len(out.Failed) != 1 {
		t.Fatalf("out-of-scope resolved entries must fail, not apply: %+v", out)
	}
	if out.Failed[0].ID != "e2" {
		t.Errorf("wrong entry rejected: %+v", out.Failed)
	}
}

func TestBulkApply_ResolverErrorIsReturnedNotSilentlyEmpty(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"a read failure", errors.New("db down")},
		{"a scope refusal", fmt.Errorf("%w: department", selection.ErrScopeDenied)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := &pagedResolver{err: tc.err}
			sa := &mockStageAssigner{}
			svc := withResolver(res, sa, &mockBroadcaster{})

			out := run(svc, filtered(allMatching(stageFilter("stage-a"))))

			if !errors.Is(out.err, tc.err) || out.Succeeded != 0 || len(sa.calls) != 0 {
				t.Fatalf("a failed resolve must touch nothing and surface the error, got %+v", out)
			}
		})
	}
}

func TestBulkApply_FilterWithoutAResolverOrGateFailsClosed(t *testing.T) {
	cases := []struct {
		name     string
		resolver selection.Resolver
		gate     cache.Gate
	}{
		{"no resolver", nil, openGate{}},
		{"no gate", &pagedResolver{ids: idsNamed(2)}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sa := &mockStageAssigner{}
			svc := NewService(sa, &mockLabelAssigner{}, &mockLabelRemover{},
				&mockEntryAssigner{}, allowAll(), &mockBroadcaster{})
			svc.SetSelection(tc.resolver, tc.gate)

			out := run(svc, filtered(allMatching(stageFilter("stage-a"))))

			if !errors.Is(out.err, selection.ErrResolverUnavailable) || len(sa.calls) != 0 {
				t.Fatalf("expected ErrResolverUnavailable with nothing touched, got %+v", out)
			}
		})
	}
}

func TestBulkApply_TheConfirmingCountWaitsForTheGate(t *testing.T) {
	res := &pagedResolver{ids: idsNamed(2)}
	sa := &mockStageAssigner{}
	svc := NewService(sa, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}, allowAll(), &mockBroadcaster{})
	svc.SetSelection(res, busyGate{})

	out := run(svc, filtered(allMatching(stageFilter("stage-a"))))

	if !errors.Is(out.err, cache.ErrGateBusy) || len(res.countCalls)+len(res.calls) != 0 || len(sa.calls) != 0 {
		t.Fatalf("a busy gate must refuse before counting, got %+v %v", out.BulkResult, out.err)
	}
}

func TestCount_ReturnsTheFingerprintOfTheCountedFilter(t *testing.T) {
	res := &pagedResolver{ids: idsNamed(7)}
	acquired := 0
	svc := NewService(&mockStageAssigner{}, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}, allowAll(), &mockBroadcaster{})
	svc.SetSelection(res, openGate{acquired: &acquired})
	scope := selection.Scope{WorkspaceID: "ws-1", ActorID: "a"}

	cases := []struct {
		name   string
		filter crmfilter.Filter
		mode   selection.Mode
	}{
		{"a filter counts all matching", *stageFilter("stage-a"), selection.ModeAllMatching},
		{"no filter counts everyone in scope", crmfilter.Filter{}, selection.ModeEveryone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matched, fp, err := svc.Count(context.Background(), scope, tc.filter)
			if err != nil || matched != 7 || fp != selection.Fingerprint(tc.filter) {
				t.Fatalf("got %d %q %v", matched, fp, err)
			}
			if last := res.countCalls[len(res.countCalls)-1]; last.Mode != tc.mode {
				t.Fatalf("counted as %q, want %q", last.Mode, tc.mode)
			}
		})
	}
	if acquired != len(cases) {
		t.Fatalf("every count passes the gate, acquired %d times", acquired)
	}
}

func TestCount_RefusesAnInvalidFilterAMissingResolverOrABusyGate(t *testing.T) {
	bogus := crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{{Field: "bogus", Operator: crmfilter.OpEquals, Values: []string{"x"}}}}}}
	svc := withResolver(&pagedResolver{}, &mockStageAssigner{}, &mockBroadcaster{})
	if _, _, err := svc.Count(context.Background(), selection.Scope{}, bogus); !errors.Is(err, crmfilter.ErrUnknownField) {
		t.Fatalf("want ErrUnknownField, got %v", err)
	}
	unstated := crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldStage, Operator: crmfilter.OpIn, Values: []string{"stage-a"}}}}}}
	counting := &pagedResolver{ids: idsNamed(3)}
	if _, _, err := withResolver(counting, &mockStageAssigner{}, &mockBroadcaster{}).Count(context.Background(), selection.Scope{}, unstated); !errors.Is(err, crmfilter.ErrConjunctionRequired) || len(counting.countCalls) != 0 {
		t.Fatalf("a group without a conjunction must be refused at preview, got %v after %d counts", err, len(counting.countCalls))
	}
	bare := NewService(&mockStageAssigner{}, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}, allowAll(), nil)
	if _, _, err := bare.Count(context.Background(), selection.Scope{}, crmfilter.Filter{}); !errors.Is(err, selection.ErrResolverUnavailable) {
		t.Fatalf("want ErrResolverUnavailable, got %v", err)
	}
	busy := &pagedResolver{ids: idsNamed(2)}
	gated := NewService(&mockStageAssigner{}, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}, allowAll(), nil)
	gated.SetSelection(busy, busyGate{})
	if _, _, err := gated.Count(context.Background(), selection.Scope{}, crmfilter.Filter{}); !errors.Is(err, cache.ErrGateBusy) || len(busy.countCalls) != 0 {
		t.Fatalf("want ErrGateBusy before counting, got %v", err)
	}
}

func TestBulkApply_ResolvesTheActorAccessOncePerCall(t *testing.T) {
	res := &pagedResolver{ids: idsNamed(1200)}
	authz := allowAll()
	authz.denyEntry = map[string]bool{res.ids[7]: true}
	sa := &mockStageAssigner{}
	svc := NewService(sa, &mockLabelAssigner{}, &mockLabelRemover{}, &mockEntryAssigner{}, authz, &mockBroadcaster{})
	svc.SetSelection(res, openGate{})

	out := run(svc, filtered(allMatching(stageFilter("stage-a"))))

	if out.err != nil || out.Succeeded != 1199 || len(out.Failed) != 1 {
		t.Fatalf("every entry is still checked on its own, got %+v %v", out.BulkResult, out.err)
	}
	if authz.actorScopes != 1 || len(authz.entryCalls) != 1200 {
		t.Fatalf("the actor scope is resolved once and each entry checked against it, got %d scopes and %d checks", authz.actorScopes, len(authz.entryCalls))
	}
}
