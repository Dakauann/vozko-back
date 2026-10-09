package crmfilter

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestAreaAcceptsOnlyTheInOperator(t *testing.T) {
	areaID := "4f1c2a8e-6b0d-4d55-9a57-2f3c8b1d0e11"
	tests := []struct {
		name string
		op   Operator
		want error
	}{
		{"in is the one membership an area answers", OpIn, nil},
		{"not_in would match every lead without a precise position", OpNotIn, ErrUnsupportedOp},
		{"neq is the same negation", OpNotEquals, ErrUnsupportedOp},
		{"eq is not offered", OpEquals, ErrUnsupportedOp},
		{"is_empty would match everyone outside every area", OpIsEmpty, ErrUnsupportedOp},
		{"is_set is not offered", OpIsSet, ErrUnsupportedOp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Predicate{Field: FieldArea, Operator: tt.op, Values: []string{areaID}}.Validate()
			if !errors.Is(err, tt.want) {
				t.Fatalf("Validate() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestAreaNeedsAtLeastOneArea(t *testing.T) {
	err := Predicate{Field: FieldArea, Operator: OpIn, Values: []string{" "}}.Validate()
	if !errors.Is(err, ErrMissingValue) {
		t.Fatalf("Validate() = %v, want ErrMissingValue", err)
	}
}

func TestAreaRefusesAnIDThatIsNotAUUID(t *testing.T) {
	err := Predicate{Field: FieldArea, Operator: OpIn, Values: []string{"north"}}.Validate()
	if !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("Validate() = %v, want ErrInvalidValue", err)
	}
}

func TestAreaTakesAtMostTheAreaCapInOnePredicate(t *testing.T) {
	ids := make([]string, MaxAreas+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("4f1c2a8e-6b0d-4d55-9a57-%012x", i)
	}
	if err := (Predicate{Field: FieldArea, Operator: OpIn, Values: ids[:MaxAreas]}).Validate(); err != nil {
		t.Fatalf("%d areas = %v, want accepted", MaxAreas, err)
	}
	if err := (Predicate{Field: FieldArea, Operator: OpIn, Values: ids}).Validate(); !errors.Is(err, ErrTooManyValues) {
		t.Fatalf("%d areas = %v, want ErrTooManyValues", len(ids), err)
	}
}

func TestBoundAreasTravelOnACopyAndNeverThroughJSON(t *testing.T) {
	areaID := "4f1c2a8e-6b0d-4d55-9a57-2f3c8b1d0e11"
	original := Predicate{Field: FieldArea, Operator: OpIn, Values: []string{areaID}}
	bounds := []AreaBounds{{ID: areaID, South: -23.6, West: -46.7, North: -23.5, East: -46.6}}
	bound := original.BindAreas(bounds)
	bounds[0].South = 0

	kind, ok := bound.BoundKind()
	if !ok || kind != KindIDSet {
		t.Fatalf("BoundKind() = %v, %v; want KindIDSet, true", kind, ok)
	}
	got := bound.BoundAreas()
	if len(got) != 1 || got[0].ID != areaID || got[0].South != -23.6 {
		t.Fatalf("BoundAreas() = %+v, want the bounds as given at binding", got)
	}
	got[0].North = 0
	if bound.BoundAreas()[0].North != -23.5 {
		t.Fatal("the bound areas must not change through a returned slice")
	}
	if len(original.BoundAreas()) != 0 {
		t.Fatal("binding must not change the original predicate")
	}
	raw, err := json.Marshal(bound)
	if err != nil {
		t.Fatal(err)
	}
	var back Predicate
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.BoundAreas()) != 0 {
		t.Fatal("a decoded predicate must be bound again by the server")
	}
}

func TestAFilterListsTheAreasBoundAcrossItsGroupsWithTheirEditTime(t *testing.T) {
	north, south := "4f1c2a8e-6b0d-4d55-9a57-2f3c8b1d0e11", "9a2b3c4d-1e2f-4a5b-8c6d-7e8f9a0b1c2d"
	edited := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	first := AreaBounds{ID: north, South: -23.6, West: -46.7, North: -23.5, East: -46.6, UpdatedAt: edited}
	second := AreaBounds{ID: south, South: -23.9, West: -46.9, North: -23.8, East: -46.8, UpdatedAt: edited.Add(time.Minute)}
	f := Filter{Groups: []Group{
		{Predicates: []Predicate{{Field: FieldBlocked, Operator: OpIsTrue}, Predicate{Field: FieldArea, Operator: OpIn, Values: []string{north}}.BindAreas([]AreaBounds{first})}},
		{Predicates: []Predicate{Predicate{Field: FieldArea, Operator: OpIn, Values: []string{south}}.BindAreas([]AreaBounds{second})}},
	}}
	got := f.BoundAreas()
	if len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("BoundAreas() = %+v, want both areas in filter order with their edit time", got)
	}
	if unbound := (Filter{Groups: []Group{{Predicates: []Predicate{{Field: FieldArea, Operator: OpIn, Values: []string{north}}}}}}).BoundAreas(); len(unbound) != 0 {
		t.Fatalf("an unbound filter lists %+v, want none", unbound)
	}
}

func TestAnAreaHoldsApproximatePositionsUnlessItsKeyAsksForExactPositionsOnly(t *testing.T) {
	areaID := "4f1c2a8e-6b0d-4d55-9a57-2f3c8b1d0e11"
	tests := []struct {
		name  string
		field Field
		key   string
		want  []GeoPlacement
		err   error
	}{
		{"an area without a key holds house and approximate positions", FieldArea, "", []GeoPlacement{PlacementOnMap, PlacementApproximate}, nil},
		{"an area keyed exact_only holds only house positions", FieldArea, AreaExactOnly, []GeoPlacement{PlacementOnMap}, nil},
		{"spaces around the key are read as the key", FieldArea, " " + AreaExactOnly + " ", []GeoPlacement{PlacementOnMap}, nil},
		{"the retired with_approximate key refuses", FieldArea, "with_approximate", nil, ErrAreaMembershipInvalid},
		{"an unknown key refuses instead of reading every position", FieldArea, "everyone", nil, ErrAreaMembershipInvalid},
		{"the left out field holds only approximate positions", FieldAreaApproximate, "", []GeoPlacement{PlacementApproximate}, nil},
		{"the left out field takes no key", FieldAreaApproximate, AreaExactOnly, nil, ErrAreaMembershipInvalid},
		{"a field that is not an area has no area membership", FieldCity, "", nil, ErrAreaMembershipInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Predicate{Field: tt.field, Key: tt.key, Operator: OpIn, Values: []string{areaID}}
			got, err := AreaPlacements(p)
			if !errors.Is(err, tt.err) {
				t.Fatalf("AreaPlacements() error = %v, want %v", err, tt.err)
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("AreaPlacements() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnAreaWithAnUnknownMembershipKeyFailsValidationAsAnInvalidFilter(t *testing.T) {
	areaID := "4f1c2a8e-6b0d-4d55-9a57-2f3c8b1d0e11"
	tests := []struct {
		name string
		p    Predicate
		want error
	}{
		{"every position", Predicate{Field: FieldArea, Operator: OpIn, Values: []string{areaID}}, nil},
		{"exact only", Predicate{Field: FieldArea, Key: AreaExactOnly, Operator: OpIn, Values: []string{areaID}}, nil},
		{"unknown key", Predicate{Field: FieldArea, Key: "all", Operator: OpIn, Values: []string{areaID}}, ErrInvalidValue},
		{"keyed left out", Predicate{Field: FieldAreaApproximate, Key: AreaExactOnly, Operator: OpIn, Values: []string{areaID}}, ErrInvalidValue},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.p.Validate(); !errors.Is(err, tt.want) {
				t.Fatalf("Validate() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestTheAreaMembershipKeyTravelsInTheFilterJSON(t *testing.T) {
	f := Filter{Groups: []Group{{Conjunction: And, Predicates: []Predicate{{Field: FieldArea, Key: AreaExactOnly, Operator: OpIn, Values: []string{"4f1c2a8e-6b0d-4d55-9a57-2f3c8b1d0e11"}}}}}}
	encoded, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	var back Filter
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatal(err)
	}
	if back.Groups[0].Predicates[0].Key != AreaExactOnly {
		t.Fatalf("decoded key = %q from %s, want %q", back.Groups[0].Predicates[0].Key, encoded, AreaExactOnly)
	}
}

func TestOnlyAnAreaKeyedExactOnlyLeavesApproximatePositionsOut(t *testing.T) {
	areaID := "4f1c2a8e-6b0d-4d55-9a57-2f3c8b1d0e11"
	tests := []struct {
		name string
		p    Predicate
		want bool
	}{
		{"an area without a key", Predicate{Field: FieldArea, Operator: OpIn, Values: []string{areaID}}, false},
		{"an area keyed exact_only", Predicate{Field: FieldArea, Key: AreaExactOnly, Operator: OpIn, Values: []string{areaID}}, true},
		{"spaces around the key", Predicate{Field: FieldArea, Key: " " + AreaExactOnly + " ", Operator: OpIn, Values: []string{areaID}}, true},
		{"the left out field", Predicate{Field: FieldAreaApproximate, Operator: OpIn, Values: []string{areaID}}, false},
		{"a field that is not an area", Predicate{Field: FieldCity, Key: AreaExactOnly, Operator: OpIn, Values: []string{"sp:sao paulo"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AreaExactOnlyOf(tt.p); got != tt.want {
				t.Fatalf("AreaExactOnlyOf() = %v, want %v", got, tt.want)
			}
		})
	}
}
