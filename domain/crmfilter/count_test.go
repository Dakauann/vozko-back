package crmfilter

import (
	"errors"
	"testing"
)

func TestACountPredicateKnowsWhetherAnEmptyCountMatches(t *testing.T) {
	cases := []struct {
		name string
		p    Predicate
		want bool
	}{
		{"equals zero", Predicate{Field: FieldCampaignCount, Operator: OpEquals, Values: []string{"0"}}, true},
		{"equals two", Predicate{Field: FieldCampaignCount, Operator: OpEquals, Values: []string{"2"}}, false},
		{"not equals zero", Predicate{Field: FieldCampaignCount, Operator: OpNotEquals, Values: []string{"0"}}, false},
		{"not equals two", Predicate{Field: FieldCampaignCount, Operator: OpNotEquals, Values: []string{"2"}}, true},
		{"at least one", Predicate{Field: FieldMemoryCount, Operator: OpGreaterEq, Values: []string{"1"}}, false},
		{"at least zero", Predicate{Field: FieldMemoryCount, Operator: OpGreaterEq, Values: []string{"0"}}, true},
		{"at most three", Predicate{Field: FieldMemoryCount, Operator: OpLessEq, Values: []string{"3"}}, true},
		{"at most minus one", Predicate{Field: FieldMemoryCount, Operator: OpLessEq, Values: []string{"-1"}}, false},
		{"between two and three", Predicate{Field: FieldCampaignCount, Operator: OpBetween, Values: []string{"2", "3"}}, false},
		{"between zero and three", Predicate{Field: FieldCampaignCount, Operator: OpBetween, Values: []string{" 0 ", "3"}}, true},
		{"is set", Predicate{Field: FieldCampaignCount, Operator: OpIsSet}, true},
		{"is empty", Predicate{Field: FieldCampaignCount, Operator: OpIsEmpty}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.p.MatchesAnEmptyCount()
			if err != nil || got != tc.want {
				t.Fatalf("MatchesAnEmptyCount() = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestACountPredicateRefusesWhatItCannotDecide(t *testing.T) {
	cases := []struct {
		name string
		p    Predicate
		want error
	}{
		{"text value", Predicate{Field: FieldCampaignCount, Operator: OpEquals, Values: []string{"many"}}, ErrInvalidNumber},
		{"no value", Predicate{Field: FieldCampaignCount, Operator: OpGreaterEq}, ErrMissingValue},
		{"one bound", Predicate{Field: FieldCampaignCount, Operator: OpBetween, Values: []string{"1"}}, ErrBetweenValues},
		{"contains", Predicate{Field: FieldCampaignCount, Operator: OpContains, Values: []string{"1"}}, ErrUnsupportedOp},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.p.MatchesAnEmptyCount(); !errors.Is(err, tc.want) {
				t.Fatalf("MatchesAnEmptyCount() error = %v, want %v", err, tc.want)
			}
		})
	}
}
