package crmfilter

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func pred(f Field, op Operator, values ...string) Predicate {
	return Predicate{Field: f, Operator: op, Values: values}
}

func filterOf(preds ...Predicate) Filter {
	return Filter{Groups: []Group{{Predicates: preds}}}
}

func TestValidate_OK(t *testing.T) {
	f := Filter{Groups: []Group{
		{Predicates: []Predicate{
			pred(FieldStage, OpIn, "s1", "s2"),
			pred(FieldValue, OpBetween, "100", "5000"),
		}},
		{Conjunction: Or, Predicates: []Predicate{
			pred(FieldSource, OpEquals, "whatsapp"),
			pred(FieldSource, OpEquals, "instagram"),
		}},
		{Predicates: []Predicate{
			pred(FieldCreatedAt, OpAfter, "2026-01-01"),
			pred(FieldCloseDate, OpBefore, "2026-12-31T23:59:59Z"),
			pred(FieldUnread, OpIsTrue),
			{Field: FieldCustom, Key: "segmento", Operator: OpEquals, Values: []string{"enterprise"}},
		}},
	}}
	if err := f.Validate(); err != nil {
		t.Fatalf("expected valid filter, got %v", err)
	}
}

func TestValidate_Errors(t *testing.T) {
	cases := []struct {
		name string
		f    Filter
		want error
	}{
		{"unknown field", filterOf(pred("bogus", OpEquals, "x")), ErrUnknownField},
		{"unsupported op", filterOf(pred(FieldValue, OpContains, "x")), ErrUnsupportedOp},
		{"between arity", filterOf(pred(FieldValue, OpBetween, "1")), ErrBetweenValues},
		{"invalid number", filterOf(pred(FieldValue, OpEquals, "abc")), ErrInvalidNumber},
		{"invalid date", filterOf(pred(FieldCreatedAt, OpAfter, "not-a-date")), ErrInvalidDate},
		{"missing value", filterOf(pred(FieldStage, OpEquals)), ErrMissingValue},
		{"custom missing key", filterOf(Predicate{Field: FieldCustom, Operator: OpEquals, Values: []string{"x"}}), ErrMissingCustomKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.f.Validate()
			if err == nil {
				t.Fatalf("expected error %v, got nil", tc.want)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected error %v, got %v", tc.want, err)
			}
		})
	}
}

func TestValidate_PresenceOperatorsNeedNoValue(t *testing.T) {
	f := filterOf(
		pred(FieldOwner, OpIsEmpty),
		pred(FieldLabel, OpIsSet),
		pred(FieldWindowOpen, OpIsFalse),
	)
	if err := f.Validate(); err != nil {
		t.Fatalf("presence operators should not require values: %v", err)
	}
}

func TestValidate_CustomFieldSkipsStaticTyping(t *testing.T) {
	// A custom field's kind is resolved by the compiler, so "abc" for what may
	// be a numeric custom field must not fail domain validation.
	f := filterOf(Predicate{Field: FieldCustom, Key: "score", Operator: OpGreaterEq, Values: []string{"abc"}})
	if err := f.Validate(); err != nil {
		t.Fatalf("custom field should skip static typing, got %v", err)
	}
}

func TestParseDate(t *testing.T) {
	for _, v := range []string{"2026-07-12", "2026-07-12T09:30:00Z"} {
		if _, err := ParseDate(v); err != nil {
			t.Fatalf("ParseDate(%q) unexpected error: %v", v, err)
		}
	}
	if _, err := ParseDate("12/07/2026"); err == nil {
		t.Fatalf("ParseDate should reject non-ISO dates")
	}
}

func TestFieldsDedup(t *testing.T) {
	f := Filter{Groups: []Group{
		{Predicates: []Predicate{pred(FieldStage, OpIn, "a"), pred(FieldOwner, OpEquals, "u1")}},
		{Predicates: []Predicate{pred(FieldStage, OpIn, "b"), pred(FieldValue, OpGreaterEq, "10")}},
	}}
	got := f.Fields()
	want := []Field{FieldStage, FieldOwner, FieldValue}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Fields() = %v, want %v", got, want)
	}
}

func TestIsEmpty(t *testing.T) {
	if !(Filter{}).IsEmpty() {
		t.Fatal("zero filter should be empty")
	}
	if (filterOf(pred(FieldStage, OpIn, "a"))).IsEmpty() {
		t.Fatal("filter with a predicate should not be empty")
	}
}

func TestGroupConjDefault(t *testing.T) {
	if (Group{}).Conj() != Or {
		t.Fatal("group conjunction should default to Or")
	}
	if (Group{Conjunction: And}).Conj() != And {
		t.Fatal("explicit And conjunction should be preserved")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	original := Filter{Groups: []Group{
		{Conjunction: Or, Predicates: []Predicate{
			pred(FieldStatus, OpEquals, "open"),
			{Field: FieldCustom, Key: "segmento", Operator: OpIn, Values: []string{"a", "b"}},
		}},
	}}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Filter
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(original, back) {
		t.Fatalf("round trip mismatch:\n got %#v\nwant %#v", back, original)
	}
	if err := back.Validate(); err != nil {
		t.Fatalf("round-tripped filter should validate: %v", err)
	}
}
