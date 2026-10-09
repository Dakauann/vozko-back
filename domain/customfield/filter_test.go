package customfield

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/crmfilter"
)

func filterDefs() []*Definition {
	return []*Definition{
		{Key: "nota", Type: TypeText},
		{Key: "score", Type: TypeNumber},
		{Key: "visita", Type: TypeDate},
		{Key: "vip", Type: TypeBoolean},
		{Key: "segmento", Type: TypeSelect, Options: []string{"a", "b"}},
		{Key: "tags", Type: TypeMultiSelect, Options: []string{"x", "y"}},
		{Key: "classificacao", Type: TypeSelect, Options: []string{"Positivo"}, Sensitive: true, LegalBasis: "consent"},
	}
}

func customPredicate(key string, op crmfilter.Operator, values ...string) crmfilter.Predicate {
	return crmfilter.Predicate{Field: crmfilter.FieldCustom, Key: key, Operator: op, Values: values}
}

func oneGroup(preds ...crmfilter.Predicate) crmfilter.Filter {
	return crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: preds}}}
}

func TestBindFilterStampsEachCustomPredicateWithItsKind(t *testing.T) {
	cases := []struct {
		key  string
		op   crmfilter.Operator
		vals []string
		want crmfilter.Kind
	}{
		{"nota", crmfilter.OpContains, []string{"acme"}, crmfilter.KindString},
		{"score", crmfilter.OpGreaterEq, []string{"10"}, crmfilter.KindNumber},
		{"visita", crmfilter.OpBefore, []string{"2026-01-01"}, crmfilter.KindDate},
		{"vip", crmfilter.OpIsTrue, nil, crmfilter.KindBool},
		{"segmento", crmfilter.OpIn, []string{"a", "b"}, crmfilter.KindEnum},
		{"tags", crmfilter.OpEquals, []string{"x"}, crmfilter.KindMultiEnum},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			bound, err := BindFilter(oneGroup(customPredicate(tc.key, tc.op, tc.vals...)), filterDefs(), Viewer{})
			if err != nil {
				t.Fatalf("BindFilter() error = %v", err)
			}
			kind, ok := bound.Groups[0].Predicates[0].BoundKind()
			if !ok || kind != tc.want {
				t.Fatalf("BoundKind() = %v, %v; want %v, true", kind, ok, tc.want)
			}
		})
	}
}

func TestBindFilterKeepsOtherPredicatesAndTheInputUntouched(t *testing.T) {
	stage := crmfilter.Predicate{Field: crmfilter.FieldStage, Operator: crmfilter.OpIn, Values: []string{"s1"}}
	input := crmfilter.Filter{Groups: []crmfilter.Group{
		{Conjunction: crmfilter.Or, Predicates: []crmfilter.Predicate{stage, customPredicate("score", crmfilter.OpEquals, "3")}},
	}}

	bound, err := BindFilter(input, filterDefs(), Viewer{})
	if err != nil {
		t.Fatalf("BindFilter() error = %v", err)
	}
	if !reflect.DeepEqual(bound.Groups[0].Predicates[0], stage) {
		t.Fatalf("a non custom predicate changed: %#v", bound.Groups[0].Predicates[0])
	}
	if bound.Groups[0].Conjunction != crmfilter.Or {
		t.Fatalf("the group conjunction changed: %q", bound.Groups[0].Conjunction)
	}
	if _, ok := input.Groups[0].Predicates[1].BoundKind(); ok {
		t.Fatal("BindFilter must not mutate its input")
	}
}

func TestBindFilterMatchesKeysLikeDefinitionsAreStored(t *testing.T) {
	bound, err := BindFilter(oneGroup(customPredicate(" Score ", crmfilter.OpEquals, "3")), filterDefs(), Viewer{})
	if err != nil {
		t.Fatalf("BindFilter() error = %v", err)
	}
	if got := bound.Groups[0].Predicates[0].Key; got != "score" {
		t.Fatalf("Key = %q, want the stored key", got)
	}
}

func TestBindFilterWithoutCustomPredicatesNeedsNoDefinitions(t *testing.T) {
	input := oneGroup(crmfilter.Predicate{Field: crmfilter.FieldStage, Operator: crmfilter.OpIn, Values: []string{"s1"}})
	bound, err := BindFilter(input, nil, Viewer{})
	if err != nil {
		t.Fatalf("BindFilter() error = %v", err)
	}
	if !reflect.DeepEqual(bound, input) {
		t.Fatalf("BindFilter() = %#v, want %#v", bound, input)
	}
}

func TestBindFilterRefusals(t *testing.T) {
	cases := []struct {
		name   string
		pred   crmfilter.Predicate
		defs   []*Definition
		viewer Viewer
		want   error
	}{
		{"unknown key", customPredicate("nope", crmfilter.OpEquals, "1"), filterDefs(), Viewer{}, ErrFilterUnknownKey},
		{"no definitions at all", customPredicate("score", crmfilter.OpEquals, "1"), nil, Viewer{}, ErrFilterUnknownKey},
		{"contains on a number", customPredicate("score", crmfilter.OpContains, "1"), filterDefs(), Viewer{}, ErrFilterOperator},
		{"range on a select", customPredicate("segmento", crmfilter.OpGreaterEq, "a"), filterDefs(), Viewer{}, ErrFilterOperator},
		{"before on text", customPredicate("nota", crmfilter.OpBefore, "2026-01-01"), filterDefs(), Viewer{}, ErrFilterOperator},
		{"is_true on text", customPredicate("nota", crmfilter.OpIsTrue), filterDefs(), Viewer{}, ErrFilterOperator},
		{"contains on a multiselect", customPredicate("tags", crmfilter.OpContains, "x"), filterDefs(), Viewer{}, ErrFilterOperator},
		{"equals on a date", customPredicate("visita", crmfilter.OpEquals, "2026-01-01"), filterDefs(), Viewer{}, ErrFilterOperator},
		{"a number that is not one", customPredicate("score", crmfilter.OpGreaterEq, "dez"), filterDefs(), Viewer{}, ErrFilterValue},
		{"a number that is infinite", customPredicate("score", crmfilter.OpLessEq, "Infinity"), filterDefs(), Viewer{}, ErrFilterValue},
		{"a number that is nan", customPredicate("score", crmfilter.OpGreaterEq, "NaN"), filterDefs(), Viewer{}, ErrFilterValue},
		{"a number in hexadecimal", customPredicate("score", crmfilter.OpEquals, "0x1p4"), filterDefs(), Viewer{}, ErrFilterValue},
		{"a date that is not one", customPredicate("visita", crmfilter.OpAfter, "12/07/2026"), filterDefs(), Viewer{}, ErrFilterValue},
		{"a boolean that is not one", customPredicate("vip", crmfilter.OpEquals, "talvez"), filterDefs(), Viewer{}, ErrFilterValue},
		{"sensitive key for a viewer who cannot read it", customPredicate("classificacao", crmfilter.OpEquals, "Positivo"), filterDefs(), Viewer{}, ErrFilterSensitive},
		{"sensitive key presence for a viewer who cannot read it", customPredicate("classificacao", crmfilter.OpIsSet), filterDefs(), Viewer{}, ErrFilterSensitive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BindFilter(oneGroup(tc.pred), tc.defs, tc.viewer)
			if !errors.Is(err, tc.want) {
				t.Fatalf("BindFilter() error = %v, want %v", err, tc.want)
			}
			var fe *FilterError
			if !errors.As(err, &fe) {
				t.Fatalf("BindFilter() error %T is not a *FilterError", err)
			}
		})
	}
}

func TestBindFilterLetsAViewerWithThePermissionFilterOnASensitiveKey(t *testing.T) {
	bound, err := BindFilter(oneGroup(customPredicate("classificacao", crmfilter.OpEquals, "Positivo")), filterDefs(), Viewer{ReadsSensitive: true})
	if err != nil {
		t.Fatalf("BindFilter() error = %v", err)
	}
	if kind, ok := bound.Groups[0].Predicates[0].BoundKind(); !ok || kind != crmfilter.KindEnum {
		t.Fatalf("BoundKind() = %v, %v", kind, ok)
	}
}

func TestBindFilterStillRunsTheStructuralChecks(t *testing.T) {
	_, err := BindFilter(oneGroup(crmfilter.Predicate{Field: crmfilter.FieldCustom, Operator: crmfilter.OpEquals, Values: []string{"1"}}), filterDefs(), Viewer{})
	if !errors.Is(err, crmfilter.ErrMissingCustomKey) {
		t.Fatalf("BindFilter() error = %v, want ErrMissingCustomKey", err)
	}
}

func TestFilterOperatorsPerType(t *testing.T) {
	for _, ft := range []FieldType{TypeText, TypeNumber, TypeDate, TypeBoolean, TypeSelect, TypeMultiSelect} {
		ops := ft.FilterOperators()
		if len(ops) == 0 {
			t.Fatalf("%s has no filter operators", ft)
		}
		for _, op := range ops {
			f := oneGroup(customPredicate("k", op, "2026-01-01", "2026-02-01"))
			if op != crmfilter.OpBetween {
				f = oneGroup(customPredicate("k", op, "2026-01-01"))
			}
			if err := f.Validate(); err != nil {
				t.Fatalf("%s operator %q is refused by the filter registry: %v", ft, op, err)
			}
		}
	}
	if FieldType("colour").FilterOperators() != nil {
		t.Fatal("an unknown type must have no operators")
	}
}

func TestBindFilterSkipsAMissingDefinitionLikeEveryOtherIndex(t *testing.T) {
	defs := append([]*Definition{nil}, filterDefs()...)
	bound, err := BindFilter(oneGroup(customPredicate("score", crmfilter.OpGreaterEq, "10")), defs, Viewer{})
	if err != nil {
		t.Fatalf("BindFilter() error = %v", err)
	}
	if kind, ok := bound.Groups[0].Predicates[0].BoundKind(); !ok || kind != crmfilter.KindNumber {
		t.Fatalf("BoundKind() = %v, %v", kind, ok)
	}
}
