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
	cycles     []time.Time
}

func (f *fakeDirectory) GeocodingWorkspaces(_ context.Context, q geocoding.PlatformQuery, cycleStart time.Time) ([]geocoding.PlatformWorkspace, int64, error) {
	f.queries, f.cycles = append(f.queries, q), append(f.cycles, cycleStart)
	return f.workspaces, f.total, f.err
}

type fakeHistory struct {
	usage      map[string]geocoding.Usage
	months     map[string][]geocoding.MonthUsage
	usageErr   error
	historyErr error
	since      time.Time
	ids        [][]string
}

func (f *fakeHistory) UsageOf(_ context.Context, ids []string) (map[string]geocoding.Usage, error) {
	f.ids = append(f.ids, ids)
	return f.usage, f.usageErr
}

func (f *fakeHistory) History(_ context.Context, ids []string, since time.Time) (map[string][]geocoding.MonthUsage, error) {
	f.since = since
	return f.months, f.historyErr
}

type fakeCoverage struct {
	byWorkspace map[string]leadmap.Summary
	err         error
	calls       int
}

func (f *fakeCoverage) Coverage(_ context.Context, ids []string) (map[string]leadmap.Summary, error) {
	f.calls++
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
	settings  *fakeSettings
	history   *fakeHistory
	coverage  *fakeCoverage
	memo      *mapMemo
	gate      *busyGate
	now       time.Time
}

func newPlatformSetup() *platformSetup {
	now := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
	period := geocoding.PeriodAt(now)
	ceilingValue := int64(800)
	return &platformSetup{
		directory: &fakeDirectory{workspaces: []geocoding.PlatformWorkspace{{ID: "ws-1", Name: "Escola"}, {ID: "ws-2", Name: "Clínica"}}, total: 2},
		settings: &fakeSettings{byWorkspace: map[string]geocoding.Settings{
			"ws-1": {WorkspaceID: "ws-1", Provider: geocoding.ProviderOpenCage, MonthlyCeiling: &ceilingValue},
		}},
		history: &fakeHistory{
			usage: map[string]geocoding.Usage{
				"ws-1": {CycleStart: period.CycleStart, Requests: 120, Day: period.Day, DayRequests: 9},
				"ws-2": {CycleStart: period.HistoryCycles()[1], Requests: 70, Day: period.Day.AddDate(0, 0, -10), DayRequests: 4},
			},
			months: map[string][]geocoding.MonthUsage{
				"ws-1": {{CycleStart: period.CycleStart, Requests: 120}, {CycleStart: period.HistoryCycles()[1], Requests: 300}},
				"ws-2": {{CycleStart: period.HistoryCycles()[1], Requests: 70}},
			},
		},
		coverage: &fakeCoverage{byWorkspace: map[string]leadmap.Summary{
			"ws-1": {Total: 10, OnMap: 4, Approximate: 2, WithoutAddress: 4},
		}},
		memo: &mapMemo{values: map[string][]byte{}},
		gate: &busyGate{},
		now:  now,
	}
}

func (s *platformSetup) usage(t *testing.T) *PlatformUsage {
	t.Helper()
	u, err := NewPlatformUsage(PlatformUsageDeps{
		Directory: s.directory, Settings: s.settings, Usage: s.history, Coverage: s.coverage,
		Memo: s.memo, Gate: s.gate, Now: func() time.Time { return s.now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

var platformAdmin = geocoding.Editor{UserID: "admin-1", PlatformAdmin: true}

func TestNewPlatformUsageRefusesMissingDependencies(t *testing.T) {
	s := newPlatformSetup()
	full := PlatformUsageDeps{Directory: s.directory, Settings: s.settings, Usage: s.history, Coverage: s.coverage, Memo: s.memo, Gate: s.gate}
	tests := []struct {
		name string
		edit func(d *PlatformUsageDeps)
	}{
		{"no directory", func(d *PlatformUsageDeps) { d.Directory = nil }},
		{"no settings", func(d *PlatformUsageDeps) { d.Settings = nil }},
		{"no usage", func(d *PlatformUsageDeps) { d.Usage = nil }},
		{"no coverage", func(d *PlatformUsageDeps) { d.Coverage = nil }},
		{"no memo", func(d *PlatformUsageDeps) { d.Memo = nil }},
		{"no gate", func(d *PlatformUsageDeps) { d.Gate = nil }},
	}
	for _, tt := range tests {
		deps := full
		tt.edit(&deps)
		if _, err := NewPlatformUsage(deps); err == nil {
			t.Fatalf("%s: NewPlatformUsage() accepted incomplete dependencies", tt.name)
		}
	}
}

func TestPlatformUsageIsForPlatformAdminsOnly(t *testing.T) {
	for _, editor := range []geocoding.Editor{{UserID: "owner", OwnerID: "owner"}, {PlatformAdmin: true}, {}} {
		s := newPlatformSetup()
		_, err := s.usage(t).List(context.Background(), editor, geocoding.PlatformQuery{})
		if !errors.Is(err, geocoding.ErrPlatformForbidden) {
			t.Fatalf("List(%+v) = %v, want forbidden", editor, err)
		}
		if len(s.directory.queries) != 0 || s.coverage.calls != 0 {
			t.Fatalf("List(%+v) read data before refusing", editor)
		}
	}
}

func TestPlatformUsageJoinsUsageHistoryAndCoveragePerWorkspace(t *testing.T) {
	s := newPlatformSetup()
	page, err := s.usage(t).List(context.Background(), platformAdmin, geocoding.PlatformQuery{Page: 1, PageSize: 500, Search: " esc "})
	if err != nil {
		t.Fatal(err)
	}
	period := geocoding.PeriodAt(s.now)
	if q := s.directory.queries[0]; q.PageSize != geocoding.PlatformMaxPageSize || q.Search != "esc" || !s.directory.cycles[0].Equal(period.CycleStart) {
		t.Fatalf("directory query = %+v at %s, want the normalized query in the current cycle", q, s.directory.cycles[0])
	}
	if !slices.Equal(s.history.ids[0], []string{"ws-1", "ws-2"}) || !s.history.since.Equal(period.HistorySince()) {
		t.Fatalf("history read %v since %s, want the page ids since %s", s.history.ids, s.history.since, period.HistorySince())
	}
	if page.TotalItems != 2 || page.Page != 1 || page.PageSize != geocoding.PlatformMaxPageSize || len(page.Items) != 2 || !page.Period.CycleStart.Equal(period.CycleStart) {
		t.Fatalf("page = %+v", page)
	}
	first := page.Items[0]
	if first.Workspace.Name != "Escola" || first.Settings.Ceiling() != 800 || first.Usage.Requests != 120 || first.Usage.DayRequests != 9 {
		t.Fatalf("first = %+v, want Escola with its ceiling and this cycle's usage", first)
	}
	if len(first.Months) != geocoding.HistoryCycles || first.Months[0].Requests != 120 || first.Months[1].Requests != 300 {
		t.Fatalf("months = %+v, want twelve cycles with this and last month", first.Months)
	}
	if first.Coverage.Total != 10 || first.Coverage.AddressShare() != 0.6 || first.Coverage.MapShare() != 0.4 {
		t.Fatalf("coverage = %+v", first.Coverage)
	}
	second := page.Items[1]
	if second.Settings.WorkspaceID != "ws-2" || second.Settings.Enabled() || second.Usage.Requests != 0 || second.Usage.DayRequests != 0 || second.Months[1].Requests != 70 || second.Coverage.Total != 0 {
		t.Fatalf("second = %+v, want reference-only settings, nothing this cycle, last month's 70 and no leads", second)
	}
}

func TestPlatformUsageIsRememberedPerQueryAndCycle(t *testing.T) {
	s := newPlatformSetup()
	u := s.usage(t)
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
	s.now = s.now.AddDate(0, 1, 0)
	if _, err := u.List(context.Background(), platformAdmin, geocoding.PlatformQuery{Page: 2}); err != nil {
		t.Fatal(err)
	}
	if len(s.directory.queries) != 3 {
		t.Fatalf("directory read %d times, want a new computation per search and per cycle", len(s.directory.queries))
	}
}

func TestPlatformUsageFailsClosed(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name string
		edit func(s *platformSetup)
		want error
	}{
		{"the gate is busy", func(s *platformSetup) { s.gate.busy = true }, cache.ErrGateBusy},
		{"the directory fails", func(s *platformSetup) { s.directory.err = boom }, boom},
		{"settings fail", func(s *platformSetup) { s.settings.err = boom }, boom},
		{"usage fails", func(s *platformSetup) { s.history.usageErr = boom }, boom},
		{"history fails", func(s *platformSetup) { s.history.historyErr = boom }, boom},
		{"coverage fails", func(s *platformSetup) { s.coverage.err = boom }, boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newPlatformSetup()
			tt.edit(s)
			page, err := s.usage(t).List(context.Background(), platformAdmin, geocoding.PlatformQuery{})
			if !errors.Is(err, tt.want) {
				t.Fatalf("List() = %+v, %v, want %v", page, err, tt.want)
			}
			if len(s.memo.values) != 0 {
				t.Fatal("a failed read was remembered")
			}
		})
	}
}

func TestPlatformUsageSkipsReadsForAnEmptyPage(t *testing.T) {
	s := newPlatformSetup()
	s.directory.workspaces, s.directory.total = nil, 2
	page, err := s.usage(t).List(context.Background(), platformAdmin, geocoding.PlatformQuery{Page: 9})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 || page.TotalItems != 2 || s.coverage.calls != 0 || len(s.history.ids) != 0 {
		t.Fatalf("page = %+v, coverage calls %d, usage reads %d, want an empty page without further reads", page, s.coverage.calls, len(s.history.ids))
	}
}
