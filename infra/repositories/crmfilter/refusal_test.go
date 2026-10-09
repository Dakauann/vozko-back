package crmfilter

import (
	"errors"
	"fmt"
	"testing"

	"vozko/domain/crmfilter"
)

func TestCompilerRefusalsReadAsAFilterThatCannotBeApplied(t *testing.T) {
	for _, refusal := range []error{ErrUnsupportedField, ErrUnsupportedOperator, ErrUnboundCustomField} {
		wrapped := fmt.Errorf("%w: %q", refusal, "x")
		if !errors.Is(wrapped, crmfilter.ErrNotApplicable) || !errors.Is(wrapped, refusal) {
			t.Fatalf("%v must keep its identity and read as crmfilter.ErrNotApplicable", refusal)
		}
	}
}

func TestCompileRefusesAFieldTheObjectDoesNotHaveAsNotApplicable(t *testing.T) {
	desc := NewOpportunityDescriptor()
	filter := crmfilter.Filter{Groups: []crmfilter.Group{{
		Conjunction: crmfilter.And,
		Predicates:  []crmfilter.Predicate{{Field: crmfilter.FieldWindowOpen, Operator: crmfilter.OpIsTrue}},
	}}}
	if _, _, err := Compile(filter, desc, 1); !errors.Is(err, crmfilter.ErrNotApplicable) {
		t.Fatalf("want ErrNotApplicable, got %v", err)
	}
}
