package leadarea

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"vozko/domain/crmfilter"
	"vozko/domain/shared"
)

const (
	northID   = "11111111-1111-4111-8111-111111111111"
	southID   = "22222222-2222-4222-8222-222222222222"
	privateID = "33333333-3333-4333-8333-333333333333"
)

func areaFilter(conj crmfilter.Conjunction, values ...string) crmfilter.Filter {
	return crmfilter.Filter{Groups: []crmfilter.Group{{
		Conjunction: conj,
		Predicates: []crmfilter.Predicate{
			{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsFalse},
			{Field: crmfilter.FieldArea, Operator: crmfilter.OpIn, Values: values},
		},
	}}}
}

func stored(id, owner string, visibility shared.Visibility, updated time.Time) Area {
	return Area{ID: id, WorkspaceID: workspaceID, OwnerID: owner, Visibility: visibility, Name: id, Shape: rectangle(), CreatedAt: now, UpdatedAt: updated}
}

func TestIDsInCollectsEveryAreaOnce(t *testing.T) {
	f := areaFilter(crmfilter.And, northID, " "+southID+" ", northID)
	f.Groups = append(f.Groups, crmfilter.Group{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldArea, Operator: crmfilter.OpIn, Values: []string{privateID}}}})
	ids, err := IDsIn(f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != northID+","+southID+","+privateID {
		t.Fatalf("ids = %v, want each area once in order", ids)
	}
	none, err := IDsIn(crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsTrue}}}}})
	if err != nil || len(none) != 0 {
		t.Fatalf("a filter without areas names none: %v %v", none, err)
	}
}

func TestIDsInRefusesWhatAnAreaPredicateCannotSay(t *testing.T) {
	many := make([]string, MaxAreasPerFilter+1)
	for i := range many {
		many[i] = fmt.Sprintf("%08d-0000-4000-8000-000000000000", i)
	}
	tests := []struct {
		name string
		f    crmfilter.Filter
		want error
	}{
		{"more than 20 areas in one predicate", areaFilter(crmfilter.And, many...), ErrTooManyAreas},
		{"a negated area", crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldArea, Operator: crmfilter.OpNotIn, Values: []string{northID}}}}}}, ErrOperatorUnsupported},
		{"an empty area", crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldArea, Operator: crmfilter.OpIsEmpty}}}}}, ErrOperatorUnsupported},
		{"an area predicate without areas", areaFilter(crmfilter.And, " "), crmfilter.ErrMissingValue},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := IDsIn(tt.f); !errors.Is(err, tt.want) {
				t.Fatalf("IDsIn() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestBindStampsEveryAreaPredicateWithTheAreasItResolvedTo(t *testing.T) {
	updated := now.Add(time.Minute)
	found := []Area{
		stored(southID, otherID, shared.VisibilityShared, updated),
		stored(northID, ownerID, shared.VisibilityPrivate, now),
	}
	bound, stamps, err := Bind(areaFilter(crmfilter.And, northID, southID, northID), workspaceID, ownerID, found)
	if err != nil {
		t.Fatal(err)
	}
	pred := bound.Groups[0].Predicates[1]
	if _, ok := pred.BoundKind(); !ok {
		t.Fatal("the area predicate must leave the binder bound, or the compiler refuses it")
	}
	if strings.Join(pred.Values, ",") != northID+","+southID {
		t.Fatalf("values = %v, want the resolved ids once", pred.Values)
	}
	if _, ok := bound.Groups[0].Predicates[0].BoundKind(); ok {
		t.Fatal("other predicates are left alone")
	}
	if len(stamps) != 2 || stamps[0].ID != northID || stamps[1].ID != southID || !stamps[1].UpdatedAt.Equal(updated) {
		t.Fatalf("stamps = %+v, want both areas sorted by id with their edit time", stamps)
	}
	if stamps.Key() == (Stamps{{ID: northID, UpdatedAt: now}, {ID: southID, UpdatedAt: now}}).Key() {
		t.Fatal("reshaping an area must change the stamp key")
	}
}

func TestBindRefusesAnAreaTheViewerCannotUse(t *testing.T) {
	deletedOrForeign := areaFilter(crmfilter.And, northID, southID)
	found := []Area{stored(northID, ownerID, shared.VisibilityPrivate, now)}
	if _, _, err := Bind(deletedOrForeign, workspaceID, ownerID, found); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a deleted, foreign or unknown area = %v, want ErrNotFound", err)
	}
	someoneElses := []Area{stored(privateID, otherID, shared.VisibilityPrivate, now)}
	if _, _, err := Bind(areaFilter(crmfilter.And, privateID), workspaceID, ownerID, someoneElses); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another member's private area = %v, want ErrNotFound", err)
	}
	if _, _, err := Bind(areaFilter(crmfilter.And, northID), workspaceID, "", found); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an anonymous viewer reads no area: %v", err)
	}
	wrongWorkspace := []Area{{ID: northID, WorkspaceID: "elsewhere", OwnerID: ownerID, Visibility: shared.VisibilityShared, UpdatedAt: now}}
	if _, _, err := Bind(areaFilter(crmfilter.And, northID), workspaceID, ownerID, wrongWorkspace); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an area of another workspace = %v, want ErrNotFound", err)
	}
}

func TestBindLeavesAFilterWithoutAreasUntouched(t *testing.T) {
	f := crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsTrue}}}}}
	bound, stamps, err := Bind(f, workspaceID, ownerID, nil)
	if err != nil || len(stamps) != 0 || len(bound.Groups) != 1 {
		t.Fatalf("bound = %+v stamps = %v err = %v", bound, stamps, err)
	}
}

func TestTheAreaCapCountsEveryAreaPredicateOfTheFilter(t *testing.T) {
	half := make([]string, MaxAreasPerFilter/2+1)
	for i := range half {
		half[i] = fmt.Sprintf("%08d-0000-4000-8000-000000000000", i)
	}
	twice := areaFilter(crmfilter.And, half...)
	twice.Groups = append(twice.Groups, crmfilter.Group{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldArea, Operator: crmfilter.OpIn, Values: half}}})
	if _, err := IDsIn(twice); !errors.Is(err, ErrTooManyAreas) {
		t.Fatalf("IDsIn() over %d area tests = %v, want ErrTooManyAreas", 2*len(half), err)
	}
	found := make([]Area, 0, len(half))
	for _, id := range half {
		found = append(found, stored(id, ownerID, shared.VisibilityShared, now))
	}
	if _, _, err := Bind(twice, workspaceID, ownerID, found); !errors.Is(err, ErrTooManyAreas) {
		t.Fatalf("Bind() over %d area tests = %v, want ErrTooManyAreas", 2*len(half), err)
	}
	if _, err := IDsIn(areaFilter(crmfilter.And, half...)); err != nil {
		t.Fatalf("one predicate of %d areas = %v, want accepted", len(half), err)
	}
}

func TestBindCarriesTheBoundsOfEveryAreaToTheCompiler(t *testing.T) {
	found := []Area{stored(northID, ownerID, shared.VisibilityShared, now)}
	bound, _, err := Bind(areaFilter(crmfilter.And, northID), workspaceID, ownerID, found)
	if err != nil {
		t.Fatal(err)
	}
	bounds := bound.Groups[0].Predicates[1].BoundAreas()
	want := crmfilter.AreaBounds{ID: northID, South: -23.57, West: -46.67, North: -23.55, East: -46.64, UpdatedAt: now}
	if len(bounds) != 1 || bounds[0] != want {
		t.Fatalf("bounds = %+v, want %+v", bounds, want)
	}
}

func approximateAreaFilter(area, approximate string) crmfilter.Filter {
	return crmfilter.Filter{Groups: []crmfilter.Group{
		{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldArea, Operator: crmfilter.OpIn, Values: []string{area}}}},
		{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldAreaApproximate, Operator: crmfilter.OpIn, Values: []string{approximate}}}},
	}}
}

func TestApproximateAreaPredicatesResolveLikeAreas(t *testing.T) {
	f := approximateAreaFilter(northID, southID)
	ids, err := IDsIn(f)
	if err != nil || strings.Join(ids, ",") != northID+","+southID {
		t.Fatalf("IDsIn() = %v, %v, want both areas", ids, err)
	}
	found := []Area{stored(northID, ownerID, shared.VisibilityShared, now), stored(southID, otherID, shared.VisibilityShared, now)}
	bound, stamps, err := Bind(f, workspaceID, ownerID, found)
	if err != nil {
		t.Fatal(err)
	}
	approximate := bound.Groups[1].Predicates[0]
	if _, ok := approximate.BoundKind(); !ok || len(approximate.BoundAreas()) != 1 || approximate.BoundAreas()[0].ID != southID {
		t.Fatalf("the approximate area predicate must carry its bounds, got %+v", approximate.BoundAreas())
	}
	if len(stamps) != 2 {
		t.Fatalf("stamps = %+v, want both areas", stamps)
	}
	if _, _, err := Bind(approximateAreaFilter(northID, privateID), workspaceID, ownerID, append(found, stored(privateID, otherID, shared.VisibilityPrivate, now))); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another member's private area read through approximate positions = %v, want ErrNotFound", err)
	}
	negated := crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldAreaApproximate, Operator: crmfilter.OpNotIn, Values: []string{northID}}}}}}
	if _, err := IDsIn(negated); !errors.Is(err, ErrOperatorUnsupported) {
		t.Fatalf("a negated approximate area = %v, want ErrOperatorUnsupported", err)
	}
}

func TestTheAreaCapCountsApproximateAreaTestsToo(t *testing.T) {
	ids := make([]string, MaxAreasPerFilter)
	for i := range ids {
		ids[i] = fmt.Sprintf("%08d-0000-4000-8000-000000000000", i)
	}
	f := areaFilter(crmfilter.And, ids...)
	f.Groups = append(f.Groups, crmfilter.Group{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldAreaApproximate, Operator: crmfilter.OpIn, Values: ids[:1]}}})
	if _, err := IDsIn(f); !errors.Is(err, ErrTooManyAreas) {
		t.Fatalf("IDsIn() over %d area tests = %v, want ErrTooManyAreas", MaxAreasPerFilter+1, err)
	}
}

func exactOnly(f crmfilter.Filter) crmfilter.Filter {
	out := crmfilter.Filter{Groups: make([]crmfilter.Group, len(f.Groups))}
	for gi, g := range f.Groups {
		preds := make([]crmfilter.Predicate, len(g.Predicates))
		for pi, p := range g.Predicates {
			if p.Field == crmfilter.FieldArea {
				p.Key = crmfilter.AreaExactOnly
			}
			preds[pi] = p
		}
		out.Groups[gi] = crmfilter.Group{Conjunction: g.Conjunction, Predicates: preds}
	}
	return out
}

func TestLeftOutReadsEveryExactOnlyAreaThroughApproximatePositions(t *testing.T) {
	found := []Area{stored(northID, ownerID, shared.VisibilityShared, now)}
	bound, _, err := Bind(exactOnly(areaFilter(crmfilter.And, northID)), workspaceID, ownerID, found)
	if err != nil {
		t.Fatal(err)
	}
	if bound.Groups[0].Predicates[1].Key != crmfilter.AreaExactOnly {
		t.Fatal("Bind keeps the membership key of the area")
	}
	left, ok, err := LeftOut(bound)
	if err != nil || !ok {
		t.Fatal("a filter with an exact only area leaves approximate leads out")
	}
	blocked, area := left.Groups[0].Predicates[0], left.Groups[0].Predicates[1]
	if blocked.Field != crmfilter.FieldBlocked || area.Field != crmfilter.FieldAreaApproximate || area.Key != "" || strings.Join(area.Values, ",") != northID {
		t.Fatalf("left out = %+v, want the same filter with the area read through approximate positions and no key", left)
	}
	if err := area.Validate(); err != nil {
		t.Fatalf("the rewritten predicate validates, got %v", err)
	}
	if len(area.BoundAreas()) != 1 || area.BoundAreas()[0].ID != northID {
		t.Fatal("the rewritten predicate keeps the bounds of its areas")
	}
	if p := bound.Groups[0].Predicates[1]; p.Field != crmfilter.FieldArea || p.Key != crmfilter.AreaExactOnly {
		t.Fatal("LeftOut must not change the filter it reads")
	}
	if _, ok, err := LeftOut(crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsTrue}}}}}); ok || err != nil {
		t.Fatal("a filter without an area leaves nothing out")
	}
}

func TestAnAreaThatHoldsApproximatePositionsLeavesNothingOut(t *testing.T) {
	cases := map[string]crmfilter.Filter{
		"an area without a key":              areaFilter(crmfilter.And, northID),
		"an area or the owner":               areaFilter(crmfilter.Or, northID),
		"an area beside an approximate area": approximateAreaFilter(northID, southID),
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			if _, ok, err := LeftOut(f); ok || err != nil {
				t.Fatalf("LeftOut() ok %v err %v, want nothing left out and no error", ok, err)
			}
		})
	}
}

func TestLeftOutKeepsAnInclusiveAreaBesideAnExactOnlyOne(t *testing.T) {
	f := crmfilter.Filter{Groups: []crmfilter.Group{
		{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldArea, Key: crmfilter.AreaExactOnly, Operator: crmfilter.OpIn, Values: []string{northID}}}},
		{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldArea, Operator: crmfilter.OpIn, Values: []string{southID}}}},
	}}
	left, ok, err := LeftOut(f)
	if err != nil || !ok {
		t.Fatalf("LeftOut() ok %v err %v", ok, err)
	}
	if left.Groups[0].Predicates[0].Field != crmfilter.FieldAreaApproximate || left.Groups[1].Predicates[0].Field != crmfilter.FieldArea || left.Groups[1].Predicates[0].Key != "" {
		t.Fatalf("left out = %+v, want only the exact only area read through approximate positions", left)
	}
}

func TestLeftOutRefusesAnExactOnlyAreaThatSharesAnEitherGroupWithAnotherTest(t *testing.T) {
	area := func(ids ...string) crmfilter.Predicate {
		return crmfilter.Predicate{Field: crmfilter.FieldArea, Key: crmfilter.AreaExactOnly, Operator: crmfilter.OpIn, Values: ids}
	}
	inclusive := crmfilter.Predicate{Field: crmfilter.FieldArea, Operator: crmfilter.OpIn, Values: []string{southID}}
	approximate := crmfilter.Predicate{Field: crmfilter.FieldAreaApproximate, Operator: crmfilter.OpIn, Values: []string{southID}}
	owner := crmfilter.Predicate{Field: crmfilter.FieldOwner, Operator: crmfilter.OpIn, Values: []string{ownerID}}
	city := crmfilter.Predicate{Field: crmfilter.FieldCity, Operator: crmfilter.OpIn, Values: []string{"sp:sao paulo"}}
	blocked := crmfilter.Predicate{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsTrue}
	cases := []struct {
		name    string
		groups  []crmfilter.Group
		refused bool
		leaves  bool
	}{
		{"an area or the owner", []crmfilter.Group{{Conjunction: crmfilter.Or, Predicates: []crmfilter.Predicate{area(northID), owner}}}, true, false},
		{"an area and a city with no conjunction", []crmfilter.Group{{Predicates: []crmfilter.Predicate{area(northID), city}}}, true, false},
		{"an area or an approximate area", []crmfilter.Group{{Predicates: []crmfilter.Predicate{area(northID), approximate}}}, true, false},
		{"an area or an area with approximate positions", []crmfilter.Group{{Conjunction: crmfilter.Or, Predicates: []crmfilter.Predicate{area(northID), inclusive}}}, true, false},
		{"an approximate area or blocked", []crmfilter.Group{{Conjunction: crmfilter.Or, Predicates: []crmfilter.Predicate{blocked, approximate}}}, false, false},
		{"one area or another", []crmfilter.Group{{Conjunction: crmfilter.Or, Predicates: []crmfilter.Predicate{area(northID), area(southID)}}}, false, true},
		{"an area alone with no conjunction", []crmfilter.Group{{Predicates: []crmfilter.Predicate{area(northID)}}}, false, true},
		{"an area and the owner", []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{area(northID), owner}}}, false, true},
		{"either group with no area beside an area group", []crmfilter.Group{
			{Conjunction: crmfilter.Or, Predicates: []crmfilter.Predicate{owner, city}},
			{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{area(northID)}},
		}, false, true},
		{"either group with no area alone", []crmfilter.Group{{Conjunction: crmfilter.Or, Predicates: []crmfilter.Predicate{owner, city}}}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			left, ok, err := LeftOut(crmfilter.Filter{Groups: tc.groups})
			if tc.refused {
				if !errors.Is(err, ErrLeftOutUnsupported) || ok || len(left.Groups) != 0 {
					t.Fatalf("LeftOut() = %+v, %v, %v, want ErrLeftOutUnsupported", left, ok, err)
				}
				if ErrorCode(err) != "area_left_out_unsupported" || !IsInputRefusal(err) {
					t.Fatalf("code %q input refusal %v, want area_left_out_unsupported refused as input", ErrorCode(err), IsInputRefusal(err))
				}
				return
			}
			if err != nil || ok != tc.leaves {
				t.Fatalf("LeftOut() ok %v err %v, want ok %v and no error", ok, err, tc.leaves)
			}
		})
	}
}

func TestHasAreaAnswersWhetherAnyTestReadsAnArea(t *testing.T) {
	cases := []struct {
		name  string
		field crmfilter.Field
		want  bool
	}{
		{"area", crmfilter.FieldArea, true},
		{"approximate area", crmfilter.FieldAreaApproximate, true},
		{"city", crmfilter.FieldCity, false},
	}
	for _, tc := range cases {
		f := crmfilter.Filter{Groups: []crmfilter.Group{
			{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsTrue}}},
			{Predicates: []crmfilter.Predicate{{Field: tc.field, Operator: crmfilter.OpIn, Values: []string{northID}}}},
		}}
		if got := HasArea(f); got != tc.want {
			t.Fatalf("HasArea(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
	if HasArea(crmfilter.Filter{}) {
		t.Fatal("an empty filter tests no area")
	}
}
