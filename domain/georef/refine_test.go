package georef

import (
	"slices"
	"testing"
	"time"

	"vozko/domain/address"
	"vozko/domain/billing"
	"vozko/domain/geo"
)

const (
	boaVista = "1400100"
	school   = "ws-school"
	clinic   = "ws-clinic"
	gym      = "ws-gym"
)

func locatedIn(workspace, district string, lat, lng float64) LocatedAddress {
	return LocatedAddress{
		ID:          "a",
		WorkspaceID: workspace,
		Postal:      address.Postal{District: district, City: "Boa Vista", State: "RR"},
		Point:       geo.Point{Lat: lat, Lng: lng},
		Precision:   geo.PrecisionExact,
		Source:      geo.SourceManual,
	}
}

func located(district string, lat, lng float64) LocatedAddress {
	return locatedIn(school, district, lat, lng)
}

func TestOnlyPositionsThatPinAHouseAndDidNotComeFromTheReferenceRefine(t *testing.T) {
	tests := []struct {
		name string
		edit func(a *LocatedAddress)
		want bool
	}{
		{"a manual pin", func(*LocatedAddress) {}, true},
		{"a location the lead sent", func(a *LocatedAddress) { a.Source = geo.SourceLeadPin }, true},
		{"imported coordinates", func(a *LocatedAddress) { a.Source = geo.SourceImport }, true},
		{"a provider match on the street", func(a *LocatedAddress) { a.Source, a.Precision = geo.SourceProvider, geo.PrecisionStreet }, true},
		{"a reference CEP point, even a tight one", func(a *LocatedAddress) { a.Source, a.Precision = geo.SourceReference, geo.PrecisionStreet }, false},
		{"a provider postal code match", func(a *LocatedAddress) { a.Source, a.Precision = geo.SourceProvider, geo.PrecisionPostalCode }, false},
		{"a bairro point", func(a *LocatedAddress) { a.Precision = geo.PrecisionDistrict }, false},
		{"an unknown source", func(a *LocatedAddress) { a.Source = "guess" }, false},
		{"a position outside Brazil", func(a *LocatedAddress) { a.Point = geo.Point{Lat: 40.7, Lng: -74} }, false},
		{"an empty position", func(a *LocatedAddress) { a.Point = geo.Point{} }, false},
		{"no workspace", func(a *LocatedAddress) { a.WorkspaceID = "  " }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := located("Centro", 2.82, -60.67)
			tt.edit(&a)
			if got := a.Refines(); got != tt.want {
				t.Fatalf("Refines() = %v, want %v for %+v", got, tt.want, a)
			}
		})
	}
	for _, p := range RefiningPrecisions() {
		if !p.PinsAHouse() {
			t.Fatalf("RefiningPrecisions() lists %q, which does not pin a house", p)
		}
	}
	for _, s := range RefiningSources() {
		if s == geo.SourceReference {
			t.Fatal("RefiningSources() lists the reference itself")
		}
	}
}

func TestARefinedBairroNeedsFiveDistinctPositions(t *testing.T) {
	r := NewDistrictRefiner(DefaultLimits())
	for range 7 {
		r.Add(boaVista, located("Centro", 2.8200, -60.6700))
	}
	r.Add(boaVista, locatedIn(clinic, "Centro", 2.8300, -60.6700))
	for i := range 4 {
		r.Add(boaVista, locatedIn([]string{school, clinic}[i%2], "Pintolândia", 2.80+float64(i)*0.001, -60.70))
	}
	if points := r.Points(); len(points) != 0 {
		t.Fatalf("Points() = %+v, want none: one household of seven plus one house, and four houses, never make a bairro point", points)
	}
	r.Add(boaVista, locatedIn(gym, "Pintolandia", 2.804, -60.70))
	points := r.Points()
	if len(points) != 1 {
		t.Fatalf("Points() = %+v, want the bairro with five houses", points)
	}
	p := points[0]
	if p.CityCode != boaVista || p.DistrictKey != address.DistrictKey("Pintolândia") || p.SampleCount != 5 {
		t.Fatalf("point = %+v, want Pintolândia in Boa Vista from five positions", p)
	}
	if !near(p.Point.Lat, 2.802) || !near(p.Point.Lng, -60.70) {
		t.Fatalf("point = %+v, want the median position", p.Point)
	}
	if p.Name != "Pintolândia" {
		t.Fatalf("name = %q, want the most common spelling", p.Name)
	}
}

func TestOneWorkspaceAloneNeverMakesABairroPoint(t *testing.T) {
	r := NewDistrictRefiner(DefaultLimits())
	for i := range 50 {
		r.Add(boaVista, located("Centro", 2.80+float64(i)*0.001, -60.67))
	}
	if points := r.Points(); len(points) != 0 {
		t.Fatalf("Points() = %+v, want none from fifty houses of a single workspace", points)
	}
	r.Add(boaVista, locatedIn(clinic, "Centro", 2.90, -60.67))
	if points := r.Points(); len(points) != 0 {
		t.Fatalf("Points() = %+v, want none while the second workspace has one house, so the first counts only one", points)
	}
	for i := range 4 {
		r.Add(boaVista, locatedIn(clinic, "Centro", 2.91+float64(i)*0.001, -60.67))
	}
	points := r.Points()
	if len(points) != 1 || points[0].SampleCount != 10 {
		t.Fatalf("Points() = %+v, want one point from five houses of each workspace", points)
	}
}

func TestNoWorkspaceHoldsTheMajorityOfABairroSample(t *testing.T) {
	r := NewDistrictRefiner(DefaultLimits())
	for i := range 100 {
		r.Add(boaVista, located("Centro", 3.50+float64(i)*0.001, -61.50))
	}
	for i := range 3 {
		r.Add(boaVista, locatedIn(clinic, "Centro", 2.800+float64(i)*0.001, -60.67))
		r.Add(boaVista, locatedIn(gym, "Centro", 2.810+float64(i)*0.001, -60.67))
	}
	points := r.Points()
	if len(points) != 1 || points[0].SampleCount != 9 {
		t.Fatalf("Points() = %+v, want nine positions, three from each workspace", points)
	}
	if lat := points[0].Point.Lat; lat > 2.9 {
		t.Fatalf("median latitude = %v, want it among the two smaller workspaces, not set by the one with a hundred houses", lat)
	}
}

func TestAHouseholdCountsOncePerPosition(t *testing.T) {
	r := NewDistrictRefiner(DefaultLimits())
	for i := range 5 {
		r.Add(boaVista, located("Centro", 2.82+float64(i)*0.001, -60.67))
		r.Add(boaVista, locatedIn(clinic, "Centro", 2.82+float64(i)*0.001, -60.66))
	}
	for range 40 {
		r.Add(boaVista, located("Centro", 3.5, -61.5))
	}
	points := r.Points()
	if len(points) != 1 || points[0].SampleCount != 10 {
		t.Fatalf("Points() = %+v, want five positions from each workspace once the household is capped to the second workspace", points)
	}
	if !near(points[0].Point.Lat, 2.822) {
		t.Fatalf("median = %+v, want a household of forty to weigh as one house", points[0].Point)
	}
}

func TestRefinerSkipsWhatItCannotPlace(t *testing.T) {
	r := NewDistrictRefiner(DefaultLimits())
	tests := []struct {
		name string
		city string
		a    LocatedAddress
	}{
		{"no city code", "", located("Centro", 2.82, -60.67)},
		{"a malformed city code", "14", located("Centro", 2.82, -60.67)},
		{"no bairro", boaVista, located("  ", 2.82, -60.67)},
		{"a reference position", boaVista, func() LocatedAddress { a := located("Centro", 2.82, -60.67); a.Source = geo.SourceReference; return a }()},
		{"no workspace", boaVista, locatedIn("", "Centro", 2.82, -60.67)},
	}
	for _, tt := range tests {
		if r.Add(tt.city, tt.a) {
			t.Fatalf("%s: Add() accepted %+v", tt.name, tt.a)
		}
	}
	if !r.Add(boaVista, located("Centro", 2.82, -60.67)) {
		t.Fatal("Add() refused a placeable address")
	}
	stats := r.Stats()
	if stats.Addresses != 6 || stats.Skipped != 5 || stats.Used != 1 {
		t.Fatalf("Stats() = %+v, want 6 read, 5 skipped, 1 used", stats)
	}
}

func TestRefinerKeepsBairrosOfDifferentCitiesApart(t *testing.T) {
	r := NewDistrictRefiner(DefaultLimits())
	for i := range 5 {
		for _, ws := range []string{school, clinic} {
			r.Add(boaVista, locatedIn(ws, "Centro", 2.82+float64(i)*0.001, -60.67))
			r.Add("1302603", locatedIn(ws, "Centro", -3.13+float64(i)*0.001, -60.02))
		}
	}
	points := r.Points()
	if len(points) != 2 || points[0].CityCode != "1302603" || points[1].CityCode != boaVista {
		t.Fatalf("Points() = %+v, want one Centro per city, ordered by city code", points)
	}
}

func TestRefinerSamplesAtMostTheDistrictLimitPerWorkspace(t *testing.T) {
	r := NewDistrictRefiner(Limits{DistrictSample: 8})
	for i := range 20 {
		r.Add(boaVista, located("Centro", 2.80+float64(i)*0.001, -60.67))
		r.Add(boaVista, locatedIn(clinic, "Centro", 2.80+float64(i)*0.001, -60.66))
	}
	points := r.Points()
	if len(points) != 1 || points[0].SampleCount != 16 {
		t.Fatalf("Points() = %+v, want the sixteen positions in the sample", points)
	}
	if !near(points[0].Point.Lat, 2.8035) {
		t.Fatalf("median = %v, want the median of the eight sampled positions of each workspace", points[0].Point.Lat)
	}
}

func TestOnlyTheCensusReplacesBairroPointsOfAnotherSource(t *testing.T) {
	tests := []struct {
		source string
		want   []string
	}{
		{SourceCNEFE, []string{SourceCNEFE, SourceLeads}},
		{SourceLeads, []string{SourceLeads}},
		{"guess", nil},
		{"", nil},
	}
	for _, tt := range tests {
		if got := DistrictSourcesReplacedBy(tt.source); !slices.Equal(got, tt.want) {
			t.Fatalf("DistrictSourcesReplacedBy(%q) = %v, want %v", tt.source, got, tt.want)
		}
	}
}

func TestRefineDueOncePerBrasiliaNight(t *testing.T) {
	brt := billing.LocationBRT()
	at := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 30, 0, 0, brt) }
	tests := []struct {
		name string
		now  time.Time
		last time.Time
		want bool
	}{
		{"never run, inside the night window", at(8, 3), time.Time{}, true},
		{"never run, in the afternoon", at(8, 15), time.Time{}, false},
		{"just before the window", at(8, 2), at(7, 3), false},
		{"the last hour of the window", at(8, 5), at(7, 3), true},
		{"after the window", at(8, 6), at(7, 3), false},
		{"already run tonight", at(8, 4), at(8, 3), false},
		{"run last night", at(8, 3), at(7, 4), true},
		{"a UTC clock still on the same Brasilia night", time.Date(2026, 10, 8, 7, 0, 0, 0, time.UTC), at(8, 3).UTC(), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RefineDueAt(tt.now, tt.last); got != tt.want {
				t.Fatalf("RefineDueAt(%s, %s) = %v, want %v", tt.now, tt.last, got, tt.want)
			}
		})
	}
}
