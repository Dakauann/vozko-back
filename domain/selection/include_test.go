package selection

import (
	"errors"
	"testing"

	"vozko/domain/crmfilter"
)

func withIncluded(s Selection, ids ...string) Selection {
	s.IncludeIDs = ids
	s.Fingerprint = s.CountFingerprint()
	return s
}

func TestIncludedLeadsValidateOnTopOfEveryWideMode(t *testing.T) {
	everyone := Selection{Mode: ModeEveryone, ExpectedCount: 3}
	firstN := Selection{Mode: ModeFirstN, Filter: stageFilter("s1"), Limit: 10}
	cases := []struct {
		name string
		s    Selection
		want error
	}{
		{"all matching plus included", withIncluded(matching(stageFilter("s1")), "a"), nil},
		{"first n plus included", withIncluded(firstN, "a", "b"), nil},
		{"everyone plus included", withIncluded(everyone, "a"), nil},
		{"included and excluded together", func() Selection {
			s := withIncluded(matching(stageFilter("s1")), "a")
			s.ExcludeIDs = []string{"b"}
			return s
		}(), nil},
		{"picked ids refuse included", Selection{Mode: ModeIDs, IDs: []string{"a"}, IncludeIDs: []string{"b"}}, ErrAmbiguousSelection},
		{"included still need a filter", withIncluded(Selection{Mode: ModeAllMatching, Filter: &crmfilter.Filter{}}, "a"), ErrFilterRequired},
		{"blank included id", withIncluded(matching(stageFilter("s1")), " "), ErrInvalidIDs},
		{"too many included", withIncluded(matching(stageFilter("s1")), make([]string, MaxExplicitIDs+1)...), ErrTooManyIDs},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.s.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestIncludedLeadsAreCountedOnTheFingerprint(t *testing.T) {
	filtered := matching(stageFilter("s1"))
	if filtered.CountFingerprint() != Fingerprint(*stageFilter("s1")) {
		t.Fatal("a selection without included leads changed its fingerprint")
	}
	one := withIncluded(filtered, "A1", "b2")
	if one.CountFingerprint() == filtered.CountFingerprint() {
		t.Fatal("the included leads were left out of the fingerprint")
	}
	if one.CountFingerprint() != withIncluded(filtered, " b2 ", "a1", "a1").CountFingerprint() {
		t.Fatal("the fingerprint depends on the order, case or repeats of the included leads")
	}
	if one.CountFingerprint() == withIncluded(filtered, "a1").CountFingerprint() {
		t.Fatal("another set of included leads kept the fingerprint")
	}
	stale := one
	stale.Fingerprint = Fingerprint(*stageFilter("s1"))
	stale.ExpectedCount = 4
	if err := stale.Validate(); !errors.Is(err, ErrFingerprintMismatch) {
		t.Fatalf("a count taken before the leads were included was accepted: %v", err)
	}
}

func TestIncludedLeadsCountPastTheLimitOfFirstN(t *testing.T) {
	firstN := Selection{Mode: ModeFirstN, Filter: stageFilter("s1"), Limit: 50}
	if firstN.LimitedCount() || firstN.ConfirmableCount(120) != 50 {
		t.Fatal("first n without included leads stopped confirming its limit")
	}
	included := withIncluded(firstN, "a", "b")
	if !included.LimitedCount() {
		t.Fatal("first n with included leads does not ask the resolver to count within the limit")
	}
	if got := included.ConfirmableCount(52); got != 52 {
		t.Fatalf("ConfirmableCount = %d, want the counted 52", got)
	}
	included.ExpectedCount = 50
	if err := included.ConfirmCount(52); !errors.Is(err, ErrCountChanged) {
		t.Fatalf("a count that lost the included leads was confirmed: %v", err)
	}
	if (withIncluded(matching(stageFilter("s1")), "a")).LimitedCount() {
		t.Fatal("only first n counts within a limit")
	}
}
