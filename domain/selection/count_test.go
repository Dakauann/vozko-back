package selection

import (
	"errors"
	"testing"

	"vozko/domain/crmfilter"
)

func TestValidateForCount_TheConfirmationIsNotNeededToCount(t *testing.T) {
	firstN := Selection{Mode: ModeFirstN, Filter: stageFilter("s1"), Limit: 10}
	cases := []struct {
		name string
		s    Selection
		want error
	}{
		{"all matching without a fingerprint", Selection{Mode: ModeAllMatching, Filter: stageFilter("s1")}, nil},
		{"all matching with a stale fingerprint", Selection{Mode: ModeAllMatching, Filter: stageFilter("s1"), Fingerprint: "old"}, nil},
		{"first n without a fingerprint", firstN, nil},
		{"everyone without a count", Selection{Mode: ModeEveryone}, nil},
		{"explicit ids", Selection{Mode: ModeIDs, IDs: []string{"a"}}, nil},
		{"everyone with a filter", Selection{Mode: ModeEveryone, Filter: stageFilter("s1")}, ErrAmbiguousSelection},
		{"all matching without a filter", Selection{Mode: ModeAllMatching}, ErrFilterRequired},
		{"first n without a limit", Selection{Mode: ModeFirstN, Filter: stageFilter("s1")}, ErrLimitRequired},
		{"no mode", Selection{}, ErrUnknownMode},
		{"negative count", Selection{Mode: ModeEveryone, ExpectedCount: -1}, ErrInvalidCount},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.s.ValidateForCount(); !errors.Is(err, tc.want) {
				t.Fatalf("ValidateForCount = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidate_StillNeedsTheConfirmation(t *testing.T) {
	if err := (Selection{Mode: ModeAllMatching, Filter: stageFilter("s1")}).Validate(); !errors.Is(err, ErrFingerprintMismatch) {
		t.Fatalf("an unconfirmed filter selection was accepted: %v", err)
	}
	if err := (Selection{Mode: ModeEveryone, Fingerprint: Fingerprint(crmfilter.Filter{})}).Validate(); !errors.Is(err, ErrEveryoneUnconfirmed) {
		t.Fatalf("an unconfirmed everyone was accepted: %v", err)
	}
}

func TestConfirmableCount(t *testing.T) {
	cases := []struct {
		name    string
		s       Selection
		matched int
		want    int
	}{
		{"all matching confirms what matched", Selection{Mode: ModeAllMatching}, 120, 120},
		{"everyone confirms what matched", Selection{Mode: ModeEveryone}, 900, 900},
		{"first n confirms its limit when more matched", Selection{Mode: ModeFirstN, Limit: 50}, 120, 50},
		{"first n confirms what matched when fewer did", Selection{Mode: ModeFirstN, Limit: 500}, 120, 120},
		{"explicit ids confirm nothing", Selection{Mode: ModeIDs, IDs: []string{"a"}}, 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.ConfirmableCount(tc.matched); got != tc.want {
				t.Fatalf("ConfirmableCount = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRequireIsServerSideAndLeavesTheFingerprintAlone(t *testing.T) {
	s := matching(stageFilter("s1"))
	s.Require = &crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsFalse}}}}}
	if err := s.Validate(); err != nil {
		t.Fatalf("a server requirement broke the client fingerprint: %v", err)
	}
	s.Require = &crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsFalse}}}}}
	if err := s.Validate(); !errors.Is(err, crmfilter.ErrConjunctionRequired) {
		t.Fatalf("a requirement without a conjunction was accepted: %v", err)
	}
}

func TestValidateConfirmed_AFilteredSelectionCarriesItsCount(t *testing.T) {
	filter := stageFilter("s1")
	fingerprint := Fingerprint(*filter)
	cases := []struct {
		name string
		s    Selection
		want error
	}{
		{"all matching with its count", Selection{Mode: ModeAllMatching, Filter: filter, Fingerprint: fingerprint, ExpectedCount: 12}, nil},
		{"all matching without a count", Selection{Mode: ModeAllMatching, Filter: filter, Fingerprint: fingerprint}, ErrCountRequired},
		{"first n without a count", Selection{Mode: ModeFirstN, Filter: filter, Fingerprint: fingerprint, Limit: 5}, ErrCountRequired},
		{"everyone without a count", Selection{Mode: ModeEveryone, Fingerprint: Fingerprint(crmfilter.Filter{})}, ErrEveryoneUnconfirmed},
		{"picked ids need no count", Selection{Mode: ModeIDs, IDs: []string{"a"}}, nil},
		{"an invalid selection stays invalid", Selection{Mode: ModeAllMatching, ExpectedCount: 3}, ErrFilterRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.s.ValidateConfirmed(); !errors.Is(err, tc.want) {
				t.Fatalf("ValidateConfirmed = %v, want %v", err, tc.want)
			}
		})
	}
	if ErrorCode(ErrCountRequired) != "selection_count_required" {
		t.Fatalf("code = %q", ErrorCode(ErrCountRequired))
	}
}
