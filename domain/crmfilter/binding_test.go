package crmfilter

import (
	"encoding/json"
	"testing"
)

func TestAPredicateStartsUnbound(t *testing.T) {
	p := Predicate{Field: FieldCustom, Key: "score", Operator: OpGreaterEq, Values: []string{"10"}}
	if _, bound := p.BoundKind(); bound {
		t.Fatal("a predicate built by hand must not carry a bound kind")
	}
}

func TestBindKindReturnsABoundCopy(t *testing.T) {
	original := Predicate{Field: FieldCustom, Key: "score", Operator: OpGreaterEq, Values: []string{"10"}}
	bound := original.BindKind(KindNumber)

	kind, ok := bound.BoundKind()
	if !ok || kind != KindNumber {
		t.Fatalf("BoundKind() = %v, %v; want KindNumber, true", kind, ok)
	}
	if _, stillUnbound := original.BoundKind(); stillUnbound {
		t.Fatal("binding must not change the original predicate")
	}
}

func TestABoundKindNeverTravelsThroughJSON(t *testing.T) {
	bound := Predicate{Field: FieldCustom, Key: "score", Operator: OpEquals, Values: []string{"1"}}.BindKind(KindMultiEnum)
	raw, err := json.Marshal(bound)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Predicate
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := back.BoundKind(); ok {
		t.Fatal("a decoded predicate must be bound again by the server, never trusted from the wire")
	}

	var forged Predicate
	if err := json.Unmarshal([]byte(`{"field":"custom","key":"score","operator":"eq","values":["1"],"boundKind":3,"bound":true}`), &forged); err != nil {
		t.Fatalf("unmarshal forged: %v", err)
	}
	if _, ok := forged.BoundKind(); ok {
		t.Fatal("a client must not be able to forge a bound kind")
	}
}

func TestCustomPredicatesAcceptEveryTypedOperator(t *testing.T) {
	for _, op := range []Operator{OpBefore, OpAfter, OpIsTrue, OpIsFalse} {
		values := []string{"2026-01-01"}
		if op == OpIsTrue || op == OpIsFalse {
			values = nil
		}
		f := filterOf(Predicate{Field: FieldCustom, Key: "k", Operator: op, Values: values})
		if err := f.Validate(); err != nil {
			t.Fatalf("custom %q should pass the registry and be typed by the binder: %v", op, err)
		}
	}
}
