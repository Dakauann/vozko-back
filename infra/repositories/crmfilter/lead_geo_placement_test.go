package crmfilter

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/lib/pq"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
)

var (
	pinningArray  = pq.StringArray{"exact", "address", "street"}
	unlocatedArgs = pq.StringArray{"not_found", "ambiguous"}
	settledArgs   = pq.StringArray{"not_found", "ambiguous", "quota_exceeded", "refused"}
)

func TestEveryGeoPlacementHasTheSummaryBucketCondition(t *testing.T) {
	unlocated := "la.lead_id IS NOT NULL AND la.latitude IS NULL AND "
	cases := []struct {
		placement crmfilter.GeoPlacement
		wantSQL   string
		wantArgs  []interface{}
	}{
		{crmfilter.PlacementOnMap, "la.latitude IS NOT NULL AND la.geo_precision = ANY(?)", []interface{}{pinningArray}},
		{crmfilter.PlacementApproximate, "la.latitude IS NOT NULL AND NOT COALESCE(la.geo_precision = ANY(?), false)", []interface{}{pinningArray}},
		{crmfilter.PlacementWithoutAddress, "la.lead_id IS NULL", nil},
		{crmfilter.PlacementNotFound, unlocated + "la.geo_status = ANY(?)", []interface{}{unlocatedArgs}},
		{crmfilter.PlacementQuotaExceeded, unlocated + "la.geo_status = ?", []interface{}{"quota_exceeded"}},
		{crmfilter.PlacementRefused, unlocated + "la.geo_status = ?", []interface{}{"refused"}},
		{crmfilter.PlacementPending, unlocated + "NOT COALESCE(la.geo_status = ANY(?), false)", []interface{}{settledArgs}},
	}
	if len(cases) != len(crmfilter.GeoPlacements()) {
		t.Fatalf("every placement needs its pinned condition, %d of %d pinned", len(cases), len(crmfilter.GeoPlacements()))
	}
	for _, tc := range cases {
		t.Run(string(tc.placement), func(t *testing.T) {
			sql, args, err := GeoPlacementCondition("la", tc.placement)
			if err != nil {
				t.Fatal(err)
			}
			if sql != tc.wantSQL || !reflect.DeepEqual(args, tc.wantArgs) {
				t.Fatalf("condition = %q %#v\nwant        %q %#v", sql, args, tc.wantSQL, tc.wantArgs)
			}
			if strings.Count(sql, "?") != len(args) {
				t.Fatalf("%d placeholders for %d args", strings.Count(sql, "?"), len(args))
			}
		})
	}
	if _, _, err := GeoPlacementCondition("la", "somewhere"); !errors.Is(err, crmfilter.ErrInvalidValue) {
		t.Fatalf("an unknown placement = %v, want ErrInvalidValue", err)
	}
}

func TestEveryDeclaredGeoPlacementBuildsItsCondition(t *testing.T) {
	for _, p := range crmfilter.GeoPlacements() {
		if _, _, err := GeoPlacementCondition("la", p); err != nil {
			t.Fatalf("%q is a declared placement with no condition: %v", p, err)
		}
	}
}

func TestGeoPlacementCountsRefuseToBuildAnUnknownBucket(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("an unknown placement must stop the counts from being built, not count nothing")
		}
	}()
	GeoPlacementCountsSQL("la", crmfilter.PlacementOnMap, crmfilter.GeoPlacement("somewhere"))
}

func TestGeoStatusArrayKeepsTheStatusesInOrder(t *testing.T) {
	cases := []struct {
		name     string
		statuses []lead.GeoStatus
		want     pq.StringArray
	}{
		{"none", nil, pq.StringArray{}},
		{"unlocated", lead.UnlocatedGeoStatuses(), unlocatedArgs},
		{"settled", lead.SettledGeoStatuses(), settledArgs},
	}
	for _, tc := range cases {
		if got := GeoStatusArray(tc.statuses); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("GeoStatusArray(%s) = %#v, want %#v", tc.name, got, tc.want)
		}
	}
}

func TestGeoPlacementCompilesAsMembershipOverThePrimaryAddress(t *testing.T) {
	withoutAddress := "leads.id NOT IN (SELECT la_f.lead_id FROM lead_addresses la_f WHERE la_f.workspace_id = ? AND la_f.is_primary)"
	onMap := "la_f.latitude IS NOT NULL AND la_f.geo_precision = ANY(?)"
	approximate := "la_f.latitude IS NOT NULL AND NOT COALESCE(la_f.geo_precision = ANY(?), false)"
	pending := "la_f.lead_id IS NOT NULL AND la_f.latitude IS NULL AND NOT COALESCE(la_f.geo_status = ANY(?), false)"
	cases := []struct {
		name     string
		pred     crmfilter.Predicate
		wantSQL  string
		wantArgs []interface{}
	}{
		{"on the map", pred(crmfilter.FieldGeoPlacement, crmfilter.OpIn, "on_map"),
			primaryAddress + " AND ((" + onMap + ")))", []interface{}{testWorkspace, pinningArray}},
		{"buckets in the summary order once", pred(crmfilter.FieldGeoPlacement, crmfilter.OpIn, "pending", "approximate", "pending"),
			primaryAddress + " AND ((" + approximate + ") OR (" + pending + ")))", []interface{}{testWorkspace, pinningArray, settledArgs}},
		{"equals one bucket", pred(crmfilter.FieldGeoPlacement, crmfilter.OpEquals, "approximate"),
			primaryAddress + " AND ((" + approximate + ")))", []interface{}{testWorkspace, pinningArray}},
		{"without an address", pred(crmfilter.FieldGeoPlacement, crmfilter.OpIn, "without_address"),
			withoutAddress, []interface{}{testWorkspace}},
		{"without an address or approximate", pred(crmfilter.FieldGeoPlacement, crmfilter.OpIn, "without_address", "approximate"),
			"(" + primaryAddress + " AND ((" + approximate + "))) OR " + withoutAddress + ")", []interface{}{testWorkspace, pinningArray, testWorkspace}},
		{"every bucket but the map", pred(crmfilter.FieldGeoPlacement, crmfilter.OpNotIn, "on_map"),
			"NOT COALESCE(" + primaryAddress + " AND ((" + onMap + "))), false)", []interface{}{testWorkspace, pinningArray}},
		{"not equals one bucket", pred(crmfilter.FieldGeoPlacement, crmfilter.OpNotEquals, "without_address"),
			"NOT COALESCE(" + withoutAddress + ", false)", []interface{}{testWorkspace}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, args := compileLead(t, scopedLeadDesc(), tc.pred)
			if want := "(" + tc.wantSQL + ")"; sql != want {
				t.Fatalf("SQL\n got: %s\nwant: %s", sql, want)
			}
			if !reflect.DeepEqual(args, tc.wantArgs) {
				t.Fatalf("args = %#v, want %#v", args, tc.wantArgs)
			}
			if got := strings.Count(sql, "?"); got != len(args) {
				t.Fatalf("%d placeholders for %d args in %s", got, len(args), sql)
			}
		})
	}
}

func TestGeoPlacementRefusesWhatItCannotScope(t *testing.T) {
	f := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		pred(crmfilter.FieldGeoPlacement, crmfilter.OpIn, "on_map"),
	}}}}
	if _, _, err := Compile(f, NewLeadDescriptor(), 0); !errors.Is(err, crmfilter.ErrNotApplicable) {
		t.Fatalf("a placement without a workspace = %v, want ErrNotApplicable", err)
	}
	if _, err := (scopedLeadDesc()).Field(crmfilter.FieldGeoPlacement); err != nil {
		t.Fatalf("Field(geo_placement) = %v", err)
	}
	unknown := pred(crmfilter.FieldGeoPlacement, crmfilter.OpIn, "somewhere")
	if _, _, err := scopedLeadDesc().compileGeoPlacement(unknown); !errors.Is(err, crmfilter.ErrInvalidValue) {
		t.Fatalf("an unknown placement reaching the compiler = %v, want ErrInvalidValue", err)
	}
	empty := pred(crmfilter.FieldGeoPlacement, crmfilter.OpIn, " ")
	if _, _, err := scopedLeadDesc().compileGeoPlacement(empty); !errors.Is(err, crmfilter.ErrMissingValue) {
		t.Fatalf("a placement predicate without values = %v, want ErrMissingValue", err)
	}
	presence := pred(crmfilter.FieldGeoPlacement, crmfilter.OpIsSet)
	if _, _, err := scopedLeadDesc().compileGeoPlacement(presence); !errors.Is(err, ErrUnsupportedOperator) {
		t.Fatalf("a presence test reaching the compiler = %v, want ErrUnsupportedOperator", err)
	}
}

func boundApproximateArea(values ...string) crmfilter.Predicate {
	bounds := make([]crmfilter.AreaBounds, 0, len(values))
	for _, id := range values {
		bounds = append(bounds, boundsOf(id))
	}
	return pred(crmfilter.FieldAreaApproximate, crmfilter.OpIn, values...).BindAreas(bounds)
}

func TestAnApproximateAreaReadsTheApproximatePositionsInsideTheRing(t *testing.T) {
	sql, args := compileLead(t, scopedLeadDesc(), boundApproximateArea(areaNorth))
	ring := "(la_f.latitude BETWEEN ? AND ? AND la_f.longitude BETWEEN ? AND ?" +
		" AND CASE WHEN point(la_f.longitude, la_f.latitude) <@ (SELECT ar.ring FROM lead_areas ar WHERE ar.id = ?::uuid AND ar.workspace_id = ? AND ar.deleted_at IS NULL) THEN true ELSE false END)"
	want := "(leads.id IN (SELECT la_f.lead_id FROM lead_addresses la_f WHERE la_f.workspace_id = ? AND la_f.is_primary" +
		" AND la_f.latitude IS NOT NULL AND NOT COALESCE(la_f.geo_precision = ANY(?), false) AND (" + ring + ")))"
	if sql != want {
		t.Fatalf("approximate area sql = %q\nwant                   %q", sql, want)
	}
	north := boundsOf(areaNorth)
	wantArgs := []interface{}{testWorkspace, pinningArray, north.South, north.North, north.West, north.East, areaNorth, testWorkspace}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("approximate area args = %#v\nwant %#v", args, wantArgs)
	}
	unbound := pred(crmfilter.FieldAreaApproximate, crmfilter.OpIn, areaNorth)
	f := crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{unbound}}}}
	if _, _, err := Compile(f, scopedLeadDesc(), 0); !errors.Is(err, ErrUnboundArea) {
		t.Fatalf("an unbound approximate area = %v, want ErrUnboundArea", err)
	}
}
