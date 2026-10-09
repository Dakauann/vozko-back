package geocoding_usecase

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"vozko/domain/cache"
	"vozko/domain/geocoding"
	"vozko/domain/leadmap"
)

type fakeDirectory struct {
	workspaces []geocoding.PlatformWorkspace
	total      int64
	err        error
	queries    []geocoding.PlatformQuery
}

func (f *fakeDirectory) GeocodingWorkspaces(_ context.Context, q geocoding.PlatformQuery) ([]geocoding.PlatformWorkspace, int64, error) {
	f.queries = append(f.queries, q)
	return f.workspaces, f.total, f.err
}

type fakeCoverage struct {
	byWorkspace map[string]leadmap.Summary
	err         error
	ids         [][]string
}

func (f *fakeCoverage) Coverage(_ context.Context, ids []string) (map[string]leadmap.Summary, error) {
	f.ids = append(f.ids, ids)
	return f.byWorkspace, f.err
}

type mapMemo struct {
	values map[string][]byte
	keys   []string
}

func (m *mapMemo) Remember(ctx context.Context, key string, _ time.Duration, compute func(context.Context) ([]byte, error)) ([]byte, error) {
	m.keys = append(m.keys, key)
	if v, ok := m.values[key]; ok {
		return v, nil
	}
	v, err := compute(ctx)
	if err != nil {
		return nil, err
	}
	m.values[key] = v
	return v, nil
}

type busyGate struct {
	busy     bool
	acquired int
}

func (g *busyGate) Acquire(context.Context) (func(), error) {
	if g.busy {
		return nil, cache.ErrGateBusy
	}
	g.acquired++
	return func() {}, nil
}

type platformSetup struct {
	directory *fakeDirectory
	coverage  *fakeCoverage
	memo      *mapMemo
	gate      *busyGate
}

func newPlatformSetup() *platformSetup {
	return &platformSetup{
		directory: &fakeDirectory{workspaces: []geocoding.PlatformWorkspace{{ID: "ws-1", Name: "Escola"}, {ID: "ws-2", Name: "Clínica"}}, total: 2},
		coverage: &fakeCoverage{byWorkspace: map[string]leadmap.Summary{
			"ws-1": {Total: 10, OnMap: 4, Approximate: 2, WithoutAddress: 4},
		}},
		memo: &mapMemo{values: map[string][]byte{}},
		gate: &busyGate{},
	}
}

func (s *platformSetup) service(t *testing.T) *PlatformCoverage {
	t.Helper()
	u, err := NewPlatformCoverage(PlatformCoverageDeps{Directory: s.directory, Coverage: s.coverage, Memo: s.memo, Gate: s.gate})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

var platformAdmin = geocoding.Caller{UserID: "admin-1", PlatformAdmin: true}

func TestNewPlatformCoverageRefusesMissingDependencies(t *testing.T) {
	s := newPlatformSetup()
	full := PlatformCoverageDeps{Directory: s.directory, Coverage: s.coverage, Memo: s.memo, Gate: s.gate}
	tests := []struct {
		name string
		edit func(d *PlatformCoverageDeps)
	}{
		{"no directory", func(d *PlatformCoverageDeps) { d.Directory = nil }},
		{"no coverage", func(d *PlatformCoverageDeps) { d.Coverage = nil }},
		{"no memo", func(d *PlatformCoverageDeps) { d.Memo = nil }},
		{"no gate", func(d *PlatformCoverageDeps) { d.Gate = nil }},
	}
	for _, tt := range tests {
		deps := full
		tt.edit(&deps)
		if _, err := NewPlatformCoverage(deps); err == nil {
			t.Fatalf("%s: NewPlatformCoverage() accepted incomplete dependencies", tt.name)
		}
	}
}

func TestPlatformCoverageIsForPlatformAdminsOnly(t *testing.T) {
	for _, caller := range []geocoding.Caller{{UserID: "member"}, {PlatformAdmin: true}, {}} {
		s := newPlatformSetup()
		_, err := s.service(t).List(context.Background(), caller, geocoding.PlatformQuery{})
		if !errors.Is(err, geocoding.ErrPlatformForbidden) {
			t.Fatalf("List(%+v) = %v, want forbidden", caller, err)
		}
		if len(s.directory.queries) != 0 || len(s.coverage.ids) != 0 {
			t.Fatalf("List(%+v) read data before refusing", caller)
		}
	}
}

func TestPlatformCoverageJoinsEachWorkspaceWithItsAddressCoverage(t *testing.T) {
	s := newPlatformSetup()
	page, err := s.service(t).List(context.Background(), platformAdmin, geocoding.PlatformQuery{Page: 1, PageSize: 500, Search: " esc "})
	if err != nil {
		t.Fatal(err)
	}
	if q := s.directory.queries[0]; q.PageSize != geocoding.PlatformMaxPageSize || q.Search != "esc" {
		t.Fatalf("directory query = %+v, want the normalized query", q)
	}
	if !slices.Equal(s.coverage.ids[0], []string{"ws-1", "ws-2"}) {
		t.Fatalf("coverage read %v, want the page ids", s.coverage.ids)
	}
	if page.TotalItems != 2 || page.Page != 1 || page.PageSize != geocoding.PlatformMaxPageSize || len(page.Items) != 2 {
		t.Fatalf("page = %+v", page)
	}
	first := page.Items[0]
	if first.Workspace.Name != "Escola" || first.Coverage.Total != 10 || first.Coverage.AddressShare() != 0.6 || first.Coverage.MapShare() != 0.4 {
		t.Fatalf("first = %+v, want Escola with its coverage", first)
	}
	if second := page.Items[1]; second.Workspace.ID != "ws-2" || second.Coverage.Total != 0 {
		t.Fatalf("second = %+v, want a workspace with no leads read as empty coverage", second)
	}
}

func TestPlatformCoverageIsRememberedPerQuery(t *testing.T) {
	s := newPlatformSetup()
	u := s.service(t)
	for range 2 {
		if _, err := u.List(context.Background(), platformAdmin, geocoding.PlatformQuery{Page: 2}); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.directory.queries) != 1 || s.gate.acquired != 1 {
		t.Fatalf("directory read %d times with %d gate slots, want one computation", len(s.directory.queries), s.gate.acquired)
	}
	if _, err := u.List(context.Background(), platformAdmin, geocoding.PlatformQuery{Page: 2, Search: "x"}); err != nil {
		t.Fatal(err)
	}
	if len(s.directory.queries) != 2 {
		t.Fatalf("directory read %d times, want a new computation per search", len(s.directory.queries))
	}
}

func TestPlatformCoverageFailsClosed(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name string
		edit func(s *platformSetup)
		want error
	}{
		{"the gate is busy", func(s *platformSetup) { s.gate.busy = true }, cache.ErrGateBusy},
		{"the directory fails", func(s *platformSetup) { s.directory.err = boom }, boom},
		{"coverage fails", func(s *platformSetup) { s.coverage.err = boom }, boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newPlatformSetup()
			tt.edit(s)
			page, err := s.service(t).List(context.Background(), platformAdmin, geocoding.PlatformQuery{})
			if !errors.Is(err, tt.want) {
				t.Fatalf("List() = %+v, %v, want %v", page, err, tt.want)
			}
			if len(s.memo.values) != 0 {
				t.Fatal("a failed read was remembered")
			}
		})
	}
}

func TestPlatformCoverageSkipsReadsForAnEmptyPage(t *testing.T) {
	s := newPlatformSetup()
	s.directory.workspaces, s.directory.total = nil, 2
	page, err := s.service(t).List(context.Background(), platformAdmin, geocoding.PlatformQuery{Page: 9})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 || page.TotalItems != 2 || len(s.coverage.ids) != 0 {
		t.Fatalf("page = %+v, coverage reads %d, want an empty page without further reads", page, len(s.coverage.ids))
	}
}
