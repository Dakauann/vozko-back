package geocoding_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"vozko/domain/address"
	"vozko/domain/billing"
	"vozko/domain/geo"
	"vozko/domain/georef"
)

type fakeLocated struct {
	rows   []georef.LocatedAddress
	err    error
	pages  []string
	limits []int
}

func (f *fakeLocated) LocatedAfter(_ context.Context, after string, limit int) ([]georef.LocatedAddress, error) {
	f.pages, f.limits = append(f.pages, after), append(f.limits, limit)
	if f.err != nil {
		return nil, f.err
	}
	var out []georef.LocatedAddress
	for _, r := range f.rows {
		if r.ID > after && len(out) < limit {
			out = append(out, r)
		}
	}
	return out, nil
}

type fakeCityCodes struct {
	byCity map[string]string
	err    error
	calls  int
}

func (f *fakeCityCodes) CityCodes(_ context.Context, postals []address.Postal) (geo.ReferenceIndex, error) {
	f.calls++
	if f.err != nil {
		return geo.ReferenceIndex{}, f.err
	}
	idx := geo.ReferenceIndex{CityCodes: map[geo.CityName]string{}}
	for _, p := range postals {
		if name, ok := geo.CityNameOf(p); ok {
			if code, known := f.byCity[p.City]; known {
				idx.CityCodes[name] = code
			}
		}
	}
	return idx, nil
}

type fakeRefinements struct {
	points    []georef.DistrictPoint
	builtAt   time.Time
	droppedAt time.Time
	writeErr  error
	dropErr   error
	drops     int
}

func (f *fakeRefinements) RefineDistricts(_ context.Context, points []georef.DistrictPoint, builtAt time.Time) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.points, f.builtAt = points, builtAt
	return nil
}

func (f *fakeRefinements) DropLeadDistrictsBefore(_ context.Context, builtAt time.Time) (int64, error) {
	f.drops++
	f.droppedAt = builtAt
	return 0, f.dropErr
}

type fakeRuns struct {
	last    time.Time
	readErr error
	marked  []time.Time
	markErr error
}

func (f *fakeRuns) LastRefine(context.Context) (time.Time, error) { return f.last, f.readErr }

func (f *fakeRuns) MarkRefine(_ context.Context, at time.Time) error {
	if f.markErr != nil {
		return f.markErr
	}
	f.marked = append(f.marked, at)
	return nil
}

var tonight = time.Date(2026, 10, 9, 3, 15, 0, 0, billing.LocationBRT())

type refineSetup struct {
	located *fakeLocated
	cities  *fakeCityCodes
	writes  *fakeRefinements
	runs    *fakeRuns
	now     time.Time
}

func newRefineSetup() *refineSetup {
	located := &fakeLocated{}
	for i := range 6 {
		located.rows = append(located.rows, georef.LocatedAddress{
			ID:          fmt.Sprintf("a-%04d", i),
			WorkspaceID: []string{"ws-a", "ws-b"}[i%2],
			Postal:      address.Postal{District: "Centro", City: "Boa Vista", State: "RR"},
			Point:       geo.Point{Lat: 2.82 + float64(i)*0.001, Lng: -60.67}, Precision: geo.PrecisionExact, Source: geo.SourceManual,
		})
	}
	located.rows = append(located.rows, georef.LocatedAddress{
		ID:          "a-0100",
		WorkspaceID: "ws-a",
		Postal:      address.Postal{District: "Centro", City: "Cidade Sem Codigo", State: "RR"},
		Point:       geo.Point{Lat: 2.9, Lng: -60.6}, Precision: geo.PrecisionExact, Source: geo.SourceManual,
	})
	return &refineSetup{
		located: located,
		cities:  &fakeCityCodes{byCity: map[string]string{"Boa Vista": "1400100"}},
		writes:  &fakeRefinements{},
		runs:    &fakeRuns{last: tonight.AddDate(0, 0, -1)},
		now:     tonight,
	}
}

func (s *refineSetup) refine(t *testing.T) *DistrictRefine {
	t.Helper()
	r, err := NewDistrictRefine(DistrictRefineDeps{
		Addresses: s.located, Cities: s.cities, Districts: s.writes, Runs: s.runs,
		Now: func() time.Time { return s.now }, BatchSize: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestNewDistrictRefineRefusesMissingDependencies(t *testing.T) {
	s := newRefineSetup()
	full := DistrictRefineDeps{Addresses: s.located, Cities: s.cities, Districts: s.writes, Runs: s.runs}
	tests := []struct {
		name string
		edit func(d *DistrictRefineDeps)
	}{
		{"no addresses", func(d *DistrictRefineDeps) { d.Addresses = nil }},
		{"no city codes", func(d *DistrictRefineDeps) { d.Cities = nil }},
		{"no district store", func(d *DistrictRefineDeps) { d.Districts = nil }},
		{"no run record", func(d *DistrictRefineDeps) { d.Runs = nil }},
	}
	for _, tt := range tests {
		deps := full
		tt.edit(&deps)
		if _, err := NewDistrictRefine(deps); err == nil {
			t.Fatalf("%s: NewDistrictRefine() accepted incomplete dependencies", tt.name)
		}
	}
}

func TestDistrictRefineWritesBairrosFromEveryPageThenDropsStaleOnesAndMarksTheNight(t *testing.T) {
	s := newRefineSetup()
	if err := s.refine(t).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.located.pages) != 2 || s.located.pages[0] != "" || s.located.pages[1] != "a-0003" || s.located.limits[0] != 4 {
		t.Fatalf("pages = %v limits = %v, want keyset pages of 4 after the last id", s.located.pages, s.located.limits)
	}
	if s.cities.calls != 2 {
		t.Fatalf("city codes looked up %d times, want once per page", s.cities.calls)
	}
	if len(s.writes.points) != 1 || s.writes.points[0].CityCode != "1400100" || s.writes.points[0].SampleCount != 6 {
		t.Fatalf("written = %+v, want Centro in Boa Vista from three houses of each workspace", s.writes.points)
	}
	if !s.writes.builtAt.Equal(s.now) || !s.writes.droppedAt.Equal(s.now) {
		t.Fatalf("built at %s, dropped before %s, want both at the run time %s", s.writes.builtAt, s.writes.droppedAt, s.now)
	}
	if len(s.runs.marked) != 1 || !s.runs.marked[0].Equal(s.now) {
		t.Fatalf("marked = %v, want the night recorded once", s.runs.marked)
	}
}

func TestDistrictRefineRunsOnlyOncePerNight(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		last time.Time
	}{
		{"already run tonight", tonight.Add(time.Hour), tonight},
		{"outside the night window", tonight.Add(10 * time.Hour), tonight.AddDate(0, 0, -1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newRefineSetup()
			s.now, s.runs.last = tt.now, tt.last
			if err := s.refine(t).Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(s.located.pages) != 0 || s.writes.drops != 0 || len(s.runs.marked) != 0 {
				t.Fatalf("the refinement ran: pages %v, drops %d, marks %v", s.located.pages, s.writes.drops, s.runs.marked)
			}
		})
	}
}

func TestDistrictRefineFailsClosed(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name    string
		edit    func(s *refineSetup)
		reads   bool
		dropped bool
	}{
		{"the last run cannot be read", func(s *refineSetup) { s.runs.readErr = boom }, false, false},
		{"an address page fails", func(s *refineSetup) { s.located.err = boom }, true, false},
		{"city codes cannot be read", func(s *refineSetup) { s.cities.err = boom }, true, false},
		{"the bairros cannot be written", func(s *refineSetup) { s.writes.writeErr = boom }, true, false},
		{"stale bairros cannot be dropped", func(s *refineSetup) { s.writes.dropErr = boom }, true, true},
		{"the night cannot be recorded", func(s *refineSetup) { s.runs.markErr = boom }, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newRefineSetup()
			tt.edit(s)
			err := s.refine(t).Run(context.Background())
			if !errors.Is(err, boom) {
				t.Fatalf("Run() = %v, want the failure", err)
			}
			if (len(s.located.pages) > 0) != tt.reads {
				t.Fatalf("addresses read = %v, want %v", len(s.located.pages) > 0, tt.reads)
			}
			if (s.writes.drops > 0) != tt.dropped {
				t.Fatalf("stale bairros dropped = %v, want %v: a failed run never drops what it did not rewrite", s.writes.drops > 0, tt.dropped)
			}
			if len(s.runs.marked) != 0 {
				t.Fatalf("a failed run was recorded: %v", s.runs.marked)
			}
		})
	}
}

func TestDistrictRefineStopsAtItsBudget(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	spent, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	tests := []struct {
		name string
		ctx  context.Context
		want error
	}{
		{"cancelled", cancelled, context.Canceled},
		{"the budget is spent", spent, context.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newRefineSetup()
			err := s.refine(t).Run(tt.ctx)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Run() = %v, want %v", err, tt.want)
			}
			if !strings.Contains(err.Error(), "unfinished after 0 addresses") {
				t.Fatalf("Run() = %v, want the error to say how far the run got", err)
			}
			if s.writes.drops != 0 || len(s.runs.marked) != 0 {
				t.Fatal("an unfinished run dropped bairros or recorded the night")
			}
		})
	}
}
