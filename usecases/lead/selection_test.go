package lead_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/selection"
	"vozko/domain/shared"
)

const (
	pickedA = "1b4e28ba-2fa1-11d2-883f-0016d3cca427"
	pickedB = "6fa459ea-ee8a-3ca4-894e-db77e160355e"
)

type fakeSelectionReader struct {
	counted []lead.SelectionQuery
	paged   []lead.SelectionQuery
	frozen  []lead.SelectionQuery
	afters  []string
	count   int
	ids     []string
}

func (f *fakeSelectionReader) CountSelection(_ context.Context, q lead.SelectionQuery) (int, error) {
	f.counted = append(f.counted, q)
	return f.count, nil
}

func (f *fakeSelectionReader) SelectionPage(_ context.Context, q lead.SelectionQuery, after string, _ int) ([]string, error) {
	f.paged = append(f.paged, q)
	f.afters = append(f.afters, after)
	return f.ids, nil
}

func (f *fakeSelectionReader) FreezeSelection(_ context.Context, q lead.SelectionQuery, _ string) (lead.Frozen, error) {
	f.frozen = append(f.frozen, q)
	return lead.Frozen{Size: f.count}, nil
}

func (f *fakeSelectionReader) SnapshotPage(context.Context, string, string, string, int) ([]string, error) {
	return f.ids, nil
}

func (f *fakeSelectionReader) DropSnapshot(context.Context, string, string) error { return nil }

func (f *fakeSelectionReader) SnapshotSize(context.Context, string, string) (int, error) {
	return len(f.ids), nil
}

func (f *fakeSelectionReader) DropSnapshotsBefore(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}

func selectionResolver(t *testing.T, perms fakePermissions, reader *fakeSelectionReader) *SelectionResolver {
	t.Helper()
	r, err := NewSelectionResolver(SelectionDeps{
		Reader: reader, Snapshots: reader, Permissions: perms, Definitions: &fakeDefinitions{defs: leadDefinitions()},
		Zones: fixedZones{loc: time.UTC}, Now: func() time.Time { return cmdNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func scopeOf(a Actor) selection.Scope {
	return selection.Scope{WorkspaceID: a.WorkspaceID, ActorID: a.UserID, IsAdmin: a.IsAdmin}
}

func interestFilter() *crmfilter.Filter {
	return &crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldCustom, Key: "interesse", Operator: crmfilter.OpEquals, Values: []string{"alto"}},
	}}}}
}

func sensitiveFilter() *crmfilter.Filter {
	return &crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldCustom, Key: "classificacao", Operator: crmfilter.OpEquals, Values: []string{"positivo"}},
	}}}}
}

func notBlocked() *crmfilter.Filter {
	return &crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsFalse}}}}}
}

func TestTheSelectionCountIsTakenBeforeExclusionsAndSkips(t *testing.T) {
	reader := &fakeSelectionReader{count: 120}
	r := selectionResolver(t, fakePermissions{"leads:read": true}, reader)
	s := selection.Selection{Mode: selection.ModeFirstN, Filter: interestFilter(), Limit: 10, ExcludeIDs: []string{pickedA}, Require: notBlocked(),
		Sort: []crmfilter.Sort{{Field: "name"}}}

	n, err := r.Count(context.Background(), scopeOf(operator()), s)
	if err != nil || n != 120 {
		t.Fatalf("Count = %d, %v", n, err)
	}
	q := reader.counted[0]
	if len(q.ExcludeIDs) != 0 || !q.Require.IsEmpty() || q.Limit != 0 || q.WorkspaceID != cmdWorkspace {
		t.Fatalf("the count read %+v", q)
	}
	if _, bound := q.Filter.Groups[0].Predicates[0].BoundKind(); !bound {
		t.Fatal("the custom predicate reached the reader unbound")
	}
}

func TestResolveAppliesSkipsAndExclusionsBeforeTheLimit(t *testing.T) {
	reader := &fakeSelectionReader{ids: []string{pickedA, pickedB}}
	r := selectionResolver(t, fakePermissions{"leads:read": true}, reader)
	s := selection.Selection{Mode: selection.ModeFirstN, Filter: interestFilter(), Limit: 10, ExcludeIDs: []string{pickedA}, Require: notBlocked(),
		Sort: []crmfilter.Sort{{Field: "name", Desc: true}}}

	refs, err := r.Resolve(context.Background(), scopeOf(operator()), s, pickedA, 500)
	if err != nil {
		t.Fatal(err)
	}
	want := []selection.Ref{{ID: pickedA, Type: lead.SelectionRefType}, {ID: pickedB, Type: lead.SelectionRefType}}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("refs = %v", refs)
	}
	q := reader.paged[0]
	if q.Limit != 10 || !reflect.DeepEqual(q.ExcludeIDs, []string{pickedA}) || q.Require.IsEmpty() ||
		!reflect.DeepEqual(q.Order, []shared.Sort{{Field: string(lead.SortName), Direction: shared.SortDesc}}) || reader.afters[0] != pickedA {
		t.Fatalf("the page read %+v after %q", q, reader.afters[0])
	}
}

func TestResolveOfPickedIDsChecksEveryID(t *testing.T) {
	reader := &fakeSelectionReader{}
	r := selectionResolver(t, fakePermissions{"leads:read": true}, reader)

	if _, err := r.Resolve(context.Background(), scopeOf(operator()), selection.Selection{Mode: selection.ModeIDs, IDs: []string{pickedA, "x"}}, "", 500); !errors.Is(err, selection.ErrInvalidIDs) {
		t.Fatalf("a malformed id = %v", err)
	}
	if _, err := r.Resolve(context.Background(), scopeOf(operator()), selection.Selection{Mode: selection.ModeIDs, IDs: []string{pickedB, pickedA, pickedB}}, "", 500); err != nil {
		t.Fatal(err)
	}
	if got := reader.paged[0].IDs; !reflect.DeepEqual(got, []string{pickedB, pickedA}) {
		t.Fatalf("picked ids reached the reader as %v", got)
	}
	if _, err := r.Resolve(context.Background(), scopeOf(operator()), selection.Selection{Mode: selection.ModeIDs, IDs: []string{pickedA}}, "", selection.MaxResolvePage+1); !errors.Is(err, selection.ErrInvalidPage) {
		t.Fatalf("an unbounded page = %v", err)
	}
}

func TestTheSelectionFollowsTheViewerRules(t *testing.T) {
	reader := &fakeSelectionReader{}
	s := selection.Selection{Mode: selection.ModeAllMatching, Filter: sensitiveFilter()}

	if _, err := selectionResolver(t, fakePermissions{}, reader).Count(context.Background(), scopeOf(operator()), s); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("without leads:read = %v", err)
	}
	if _, err := selectionResolver(t, fakePermissions{"leads:read": true}, reader).Count(context.Background(), scopeOf(operator()), s); !errors.Is(err, customfield.ErrFilterSensitive) {
		t.Fatalf("a sensitive filter without the permission = %v", err)
	}
	if _, err := selectionResolver(t, withSensitive(fakePermissions{"leads:read": true}), reader).Count(context.Background(), scopeOf(operator()), s); err != nil {
		t.Fatalf("a sensitive filter with the permission = %v", err)
	}
	unknown := selection.Selection{Mode: selection.ModeFirstN, Filter: interestFilter(), Limit: 3, Sort: []crmfilter.Sort{{Field: "phone"}}}
	if _, err := selectionResolver(t, fakePermissions{"leads:read": true}, reader).Resolve(context.Background(), scopeOf(operator()), unknown, "", 10); !errors.Is(err, selection.ErrUnknownSort) {
		t.Fatalf("an unknown sort = %v", err)
	}
	if _, err := selectionResolver(t, fakePermissions{"leads:read": true}, reader).Count(context.Background(), selection.Scope{ActorID: cmdUser}, s); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Fatalf("a scope without a workspace = %v", err)
	}
}

func TestFreezeTakesTheResolvedSet(t *testing.T) {
	reader := &fakeSelectionReader{count: 3}
	r := selectionResolver(t, fakePermissions{"leads:read": true}, reader)
	s := selection.Selection{Mode: selection.ModeEveryone, ExcludeIDs: []string{pickedA}}

	frozen, err := r.Freeze(context.Background(), scopeOf(operator()), s, "snap-1", nil)
	if err != nil || frozen.Size != 3 {
		t.Fatalf("Freeze = %+v, %v", frozen, err)
	}
	if q := reader.frozen[0]; !reflect.DeepEqual(q.ExcludeIDs, []string{pickedA}) || !q.Filter.IsEmpty() {
		t.Fatalf("the freeze read %+v", q)
	}
}

func TestNewSelectionResolverRefusesMissingPorts(t *testing.T) {
	if _, err := NewSelectionResolver(SelectionDeps{}); err == nil {
		t.Fatal("a resolver without its ports must not be built")
	}
}

func TestFreezeCarriesThePendingAssignmentOfAQuantitySelection(t *testing.T) {
	reader := &fakeSelectionReader{count: 3}
	r := selectionResolver(t, fakePermissions{"leads:read": true}, reader)
	pending := &lead.Assignment{Kind: lead.AssignBlocked, Value: true}
	s := selection.Selection{Mode: selection.ModeFirstN, Filter: interestFilter(), Limit: 3}
	if _, err := r.Freeze(context.Background(), scopeOf(operator()), s, "snap-1", pending); err != nil {
		t.Fatal(err)
	}
	if q := reader.frozen[0]; q.Pending != pending || q.Limit != 3 {
		t.Fatalf("the freeze read %+v", q)
	}
}

func TestSelectedCountsAfterExclusionsAndSkipsWithinTheLimit(t *testing.T) {
	reader := &fakeSelectionReader{count: 40}
	r := selectionResolver(t, fakePermissions{"leads:read": true}, reader)
	blocked := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsFalse}}}}}
	cases := []struct {
		name string
		s    selection.Selection
		want int
	}{
		{"a filter", selection.Selection{Mode: selection.ModeAllMatching, Filter: interestFilter(), ExcludeIDs: []string{pickedA}, Require: &blocked}, 40},
		{"a quantity under what matched", selection.Selection{Mode: selection.ModeFirstN, Filter: interestFilter(), Limit: 25}, 25},
		{"a quantity over what matched", selection.Selection{Mode: selection.ModeFirstN, Filter: interestFilter(), Limit: 90}, 40},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.Selected(context.Background(), scopeOf(operator()), tc.s)
			if err != nil || got != tc.want {
				t.Fatalf("Selected = %d, %v, want %d", got, err, tc.want)
			}
			q := reader.counted[i]
			if q.Limit != 0 || len(q.ExcludeIDs) != len(tc.s.ExcludeIDs) || (tc.s.Require != nil && q.Require.IsEmpty()) {
				t.Fatalf("the count read %+v", q)
			}
		})
	}
}
