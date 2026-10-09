package crmfilter

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/lib/pq"

	"vozko/domain/crmfilter"
	"vozko/domain/geo"
)

const (
	areaNorth = "11111111-1111-4111-8111-111111111111"
	areaSouth = "22222222-2222-4222-8222-222222222222"
)

func boundsOf(id string) crmfilter.AreaBounds {
	if id == areaSouth {
		return crmfilter.AreaBounds{ID: id, South: -23.70, West: -46.75, North: -23.60, East: -46.65}
	}
	return crmfilter.AreaBounds{ID: id, South: -23.57, West: -46.67, North: -23.55, East: -46.64}
}

func boundArea(values ...string) crmfilter.Predicate {
	bounds := make([]crmfilter.AreaBounds, 0, len(values))
	for _, id := range values {
		bounds = append(bounds, boundsOf(id))
	}
	return pred(crmfilter.FieldArea, crmfilter.OpIn, values...).BindAreas(bounds)
}

func TestAreaCompilesAsUncorrelatedMembershipOverPinnedPrimaryAddresses(t *testing.T) {
	sql, args := compileLead(t, scopedLeadDesc(), boundArea(areaNorth, areaSouth))
	ring := "(la_f.latitude BETWEEN ? AND ? AND la_f.longitude BETWEEN ? AND ?" +
		" AND CASE WHEN point(la_f.longitude, la_f.latitude) <@ (SELECT ar.ring FROM lead_areas ar WHERE ar.id = ?::uuid AND ar.workspace_id = ? AND ar.deleted_at IS NULL) THEN true ELSE false END)"
	want := "(leads.id IN (SELECT la_f.lead_id FROM lead_addresses la_f WHERE la_f.workspace_id = ? AND la_f.is_primary" +
		" AND la_f.latitude IS NOT NULL AND la_f.geo_precision = ANY(?) AND (" + ring + " OR " + ring + ")))"
	if sql != want {
		t.Fatalf("area sql = %q\nwant      %q", sql, want)
	}
	pinning := make([]string, 0, 3)
	for _, p := range geo.PinningPrecisions() {
		pinning = append(pinning, string(p))
	}
	north, south := boundsOf(areaNorth), boundsOf(areaSouth)
	wantArgs := []interface{}{testWorkspace, pq.StringArray(pinning),
		north.South, north.North, north.West, north.East, areaNorth, testWorkspace,
		south.South, south.North, south.West, south.East, areaSouth, testWorkspace}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("area args = %#v\nwant       %#v", args, wantArgs)
	}
	if len(pinning) != 3 || pinning[0] != "exact" || pinning[2] != "street" {
		t.Fatalf("the precisions come from geo.PinsAHouse, got %v", pinning)
	}
}

func TestAnAreaPredicateNeverCarriesMoreThanTwentyAreas(t *testing.T) {
	ids := make([]string, crmfilter.MaxAreas+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("11111111-1111-4111-8111-%012d", i)
	}
	f := crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{boundArea(ids...)}}}}
	if _, _, err := Compile(f, scopedLeadDesc(), 0); !errors.Is(err, crmfilter.ErrTooManyValues) {
		t.Fatalf("21 areas = %v, want ErrTooManyValues", err)
	}
}

func TestAnAreaWithoutItsBoundsIsNeverCompiled(t *testing.T) {
	kindOnly := pred(crmfilter.FieldArea, crmfilter.OpIn, areaNorth).BindKind(crmfilter.KindIDSet)
	other := pred(crmfilter.FieldArea, crmfilter.OpIn, areaNorth, areaSouth).BindAreas([]crmfilter.AreaBounds{boundsOf(areaNorth)})
	for name, p := range map[string]crmfilter.Predicate{"bound kind without bounds": kindOnly, "bounds of only one area": other} {
		f := crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{p}}}}
		if _, _, err := Compile(f, scopedLeadDesc(), 0); !errors.Is(err, ErrUnboundArea) {
			t.Fatalf("%s = %v, want ErrUnboundArea", name, err)
		}
	}
}

func TestAnUnresolvedAreaIsNeverCompiled(t *testing.T) {
	f := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		pred(crmfilter.FieldArea, crmfilter.OpIn, areaNorth),
	}}}}
	_, _, err := Compile(f, scopedLeadDesc(), 0)
	if !errors.Is(err, ErrUnboundArea) || !errors.Is(err, crmfilter.ErrNotApplicable) {
		t.Fatalf("an area the use case did not resolve = %v, want ErrUnboundArea", err)
	}
}

func TestAnAreaNeedsTheWorkspaceItBelongsTo(t *testing.T) {
	f := crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{boundArea(areaNorth)}}}}
	if _, _, err := Compile(f, NewLeadDescriptor(), 0); !errors.Is(err, ErrWorkspaceScopeRequired) {
		t.Fatalf("an area without a workspace scope = %v, want ErrWorkspaceScopeRequired", err)
	}
}

func TestAnAreaIsNotAConversationOrDealField(t *testing.T) {
	f := crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{boundArea(areaNorth)}}}}
	if _, _, err := Compile(f, NewConversationDescriptor(), 0); !errors.Is(err, ErrUnsupportedField) {
		t.Fatalf("area on conversations = %v, want ErrUnsupportedField", err)
	}
}

func TestAnAreaHashedOnceUnderOrStaysUncorrelated(t *testing.T) {
	f := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.Or, Predicates: []crmfilter.Predicate{
		boundArea(areaNorth),
		pred(crmfilter.FieldBlocked, crmfilter.OpIsTrue),
	}}}}
	sql, _, err := Compile(f, scopedLeadDesc(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := "(leads.id IN (SELECT la_f.lead_id"; len(sql) < len(want) || sql[:len(want)] != want {
		t.Fatalf("area under OR = %q, want the membership first", sql)
	}
}

func TestAnAreaWithApproximateLeadsReadsHouseAndApproximatePositionsInsideTheRing(t *testing.T) {
	keyed := boundArea(areaNorth)
	keyed.Key = crmfilter.AreaWithApproximate
	sql, args := compileLead(t, scopedLeadDesc(), keyed)
	ring := "(la_f.latitude BETWEEN ? AND ? AND la_f.longitude BETWEEN ? AND ?" +
		" AND CASE WHEN point(la_f.longitude, la_f.latitude) <@ (SELECT ar.ring FROM lead_areas ar WHERE ar.id = ?::uuid AND ar.workspace_id = ? AND ar.deleted_at IS NULL) THEN true ELSE false END)"
	want := "(leads.id IN (SELECT la_f.lead_id FROM lead_addresses la_f WHERE la_f.workspace_id = ? AND la_f.is_primary" +
		" AND ((la_f.latitude IS NOT NULL AND la_f.geo_precision = ANY(?)) OR (la_f.latitude IS NOT NULL AND NOT COALESCE(la_f.geo_precision = ANY(?), false)))" +
		" AND (" + ring + ")))"
	if sql != want {
		t.Fatalf("area with approximate sql = %q\nwant                       %q", sql, want)
	}
	north := boundsOf(areaNorth)
	wantArgs := []interface{}{testWorkspace, pinningArray, pinningArray, north.South, north.North, north.West, north.East, areaNorth, testWorkspace}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("area with approximate args = %#v\nwant %#v", args, wantArgs)
	}
	if placeholders := strings.Count(sql, "?"); placeholders != len(args) {
		t.Fatalf("%d placeholders for %d args", placeholders, len(args))
	}
}

func TestAnAreaWithAnUnknownMembershipNeverCompiles(t *testing.T) {
	keyed := boundArea(areaNorth)
	keyed.Key = "everyone"
	f := crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{keyed}}}}
	if _, _, err := Compile(f, scopedLeadDesc(), 0); !errors.Is(err, crmfilter.ErrAreaMembershipInvalid) {
		t.Fatalf("an unknown area membership = %v, want ErrAreaMembershipInvalid", err)
	}
}
