package lead_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/geo"
	"vozko/domain/lead"
	"vozko/domain/leadmap"
	"vozko/domain/selection"
)

type fakePlaces struct {
	asked  []lead.SelectionQuery
	limits []int
	sample leadmap.Sample
}

func (f *fakePlaces) SelectionSample(_ context.Context, q lead.SelectionQuery, limit int) (leadmap.Sample, error) {
	f.asked = append(f.asked, q)
	f.limits = append(f.limits, limit)
	return f.sample, nil
}

func sampleResolver(t *testing.T, perms fakePermissions, places *fakePlaces, areas *mapAreas) *SelectionResolver {
	t.Helper()
	reader := &fakeSelectionReader{}
	deps := SelectionDeps{
		Reader: reader, Snapshots: reader, Permissions: perms, Definitions: &fakeDefinitions{defs: leadDefinitions()},
		Zones: fixedZones{loc: time.UTC}, Areas: areas, Now: func() time.Time { return cmdNow },
	}
	if places != nil {
		deps.Places = places
	}
	r, err := NewSelectionResolver(deps)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestASampleReadsTheSelectionOfTheCallerWithItsAreasBoundAndFramed(t *testing.T) {
	places := &fakePlaces{sample: leadmap.Sample{Located: 3, Positions: 2}}
	areas := &mapAreas{updated: time.Unix(100, 0), shape: geo.Shape{Kind: geo.ShapeCircle, Center: geo.Point{Lat: -6.31, Lng: -35.47}, RadiusM: 1000}}
	r := sampleResolver(t, mapViewer, places, areas)
	filter := areaOnly()
	s := selection.Selection{Mode: selection.ModeAllMatching, Filter: &filter, ExcludeIDs: []string{pickedA}}

	sample, err := r.Sample(context.Background(), scopeOf(operator()), s, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(places.asked) != 1 || places.limits[0] != 50 {
		t.Fatalf("the sample read %d times with limits %v", len(places.asked), places.limits)
	}
	q := places.asked[0]
	if q.WorkspaceID != cmdWorkspace || len(q.ExcludeIDs) != 1 || q.ExcludeIDs[0] != pickedA {
		t.Fatalf("the sample query = %+v, want the caller's workspace and exclusions", q)
	}
	if _, bound := q.Filter.Groups[0].Predicates[0].BoundKind(); !bound {
		t.Fatalf("the area must be bound before the read, got %+v", q.Filter)
	}
	if sample.Located != 3 || sample.Bounds == nil || sample.Bounds.South > -6.31 || sample.Bounds.North < -6.31 {
		t.Fatalf("sample = %+v, want the read framed on the area", sample)
	}
}

func TestASampleNeedsTheAddressPermissionAndAReader(t *testing.T) {
	filter := areaOnly()
	s := selection.Selection{Mode: selection.ModeAllMatching, Filter: &filter}
	places := &fakePlaces{}
	noAddresses := sampleResolver(t, fakePermissions{"leads:read": true}, places, &mapAreas{})
	if _, err := noAddresses.Sample(context.Background(), scopeOf(operator()), s, 10); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("a caller without addresses sampled: %v", err)
	}
	if len(places.asked) != 0 {
		t.Fatal("a refused sample must read nothing")
	}
	unwired := sampleResolver(t, mapViewer, nil, &mapAreas{})
	if _, err := unwired.Sample(context.Background(), scopeOf(operator()), s, 10); !errors.Is(err, selection.ErrResolverUnavailable) {
		t.Fatalf("a resolver without a reader sampled: %v", err)
	}
}
