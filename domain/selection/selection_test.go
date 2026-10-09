package selection

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/crmfilter"
)

func stageFilter(ids ...string) *crmfilter.Filter {
	return &crmfilter.Filter{Groups: []crmfilter.Group{{
		Conjunction: crmfilter.And,
		Predicates:  []crmfilter.Predicate{{Field: crmfilter.FieldStage, Operator: crmfilter.OpIn, Values: ids}},
	}}}
}

func matching(f *crmfilter.Filter) Selection {
	return Selection{Mode: ModeAllMatching, Filter: f, Fingerprint: Fingerprint(*f)}
}

func TestValidate(t *testing.T) {
	everyone := Selection{Mode: ModeEveryone, ExpectedCount: 120, Fingerprint: Fingerprint(crmfilter.Filter{})}
	firstN := matching(stageFilter("s1"))
	firstN.Mode = ModeFirstN
	firstN.Limit = 5000

	cases := []struct {
		name   string
		mutate func() Selection
		want   error
	}{
		{"explicit ids", func() Selection { return Selection{Mode: ModeIDs, IDs: []string{"a", "b"}} }, nil},
		{"all matching", func() Selection { return matching(stageFilter("s1")) }, nil},
		{"first n", func() Selection { return firstN }, nil},
		{"everyone confirmed by count", func() Selection { return everyone }, nil},
		{"everyone without a filter pointer", func() Selection { s := everyone; s.Filter = &crmfilter.Filter{}; return s }, nil},

		{"no mode", func() Selection { return Selection{IDs: []string{"a"}} }, ErrUnknownMode},
		{"unknown mode", func() Selection { return Selection{Mode: "some"} }, ErrUnknownMode},

		{"ids empty", func() Selection { return Selection{Mode: ModeIDs} }, ErrEmptySelection},
		{"ids blank", func() Selection { return Selection{Mode: ModeIDs, IDs: []string{" "}} }, ErrEmptySelection},
		{"ids with a filter", func() Selection {
			return Selection{Mode: ModeIDs, IDs: []string{"a"}, Filter: stageFilter("s1")}
		}, ErrAmbiguousSelection},
		{"ids with a limit", func() Selection { return Selection{Mode: ModeIDs, IDs: []string{"a"}, Limit: 3} }, ErrAmbiguousSelection},
		{"ids with exclusions", func() Selection {
			return Selection{Mode: ModeIDs, IDs: []string{"a"}, ExcludeIDs: []string{"b"}}
		}, ErrAmbiguousSelection},
		{"too many ids", func() Selection {
			return Selection{Mode: ModeIDs, IDs: make([]string, MaxExplicitIDs+1)}
		}, ErrTooManyIDs},

		{"all matching without filter", func() Selection { return Selection{Mode: ModeAllMatching} }, ErrFilterRequired},
		{"all matching with empty filter", func() Selection {
			return Selection{Mode: ModeAllMatching, Filter: &crmfilter.Filter{}, Fingerprint: Fingerprint(crmfilter.Filter{})}
		}, ErrFilterRequired},
		{"all matching with empty groups", func() Selection {
			f := &crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And}}}
			return Selection{Mode: ModeAllMatching, Filter: f, Fingerprint: Fingerprint(*f)}
		}, ErrFilterRequired},
		{"all matching with ids", func() Selection { s := matching(stageFilter("s1")); s.IDs = []string{"a"}; return s }, ErrAmbiguousSelection},
		{"all matching with a limit", func() Selection { s := matching(stageFilter("s1")); s.Limit = 10; return s }, ErrAmbiguousSelection},
		{"all matching with an invalid filter", func() Selection {
			f := &crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{{Field: "bogus", Operator: crmfilter.OpEquals, Values: []string{"x"}}}}}}
			return Selection{Mode: ModeAllMatching, Filter: f, Fingerprint: Fingerprint(*f)}
		}, crmfilter.ErrUnknownField},
		{"all matching with a group that states no conjunction", func() Selection {
			f := &crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{
				{Field: crmfilter.FieldStage, Operator: crmfilter.OpIn, Values: []string{"s1"}},
				{Field: crmfilter.FieldLabel, Operator: crmfilter.OpIn, Values: []string{"l1"}},
			}}}}
			return Selection{Mode: ModeAllMatching, Filter: f, Fingerprint: Fingerprint(*f)}
		}, crmfilter.ErrConjunctionRequired},
		{"all matching without fingerprint", func() Selection { s := matching(stageFilter("s1")); s.Fingerprint = ""; return s }, ErrFingerprintMismatch},
		{"all matching counted on another filter", func() Selection {
			s := matching(stageFilter("s1"))
			s.Fingerprint = Fingerprint(*stageFilter("s2"))
			return s
		}, ErrFingerprintMismatch},
		{"too many exclusions", func() Selection {
			s := matching(stageFilter("s1"))
			s.ExcludeIDs = make([]string, MaxExplicitIDs+1)
			return s
		}, ErrTooManyIDs},
		{"negative expected count", func() Selection { s := matching(stageFilter("s1")); s.ExpectedCount = -1; return s }, ErrInvalidCount},

		{"first n without limit", func() Selection { s := firstN; s.Limit = 0; return s }, ErrLimitRequired},
		{"first n above the cap", func() Selection { s := firstN; s.Limit = MaxFirstN + 1; return s }, ErrLimitRequired},
		{"first n without filter", func() Selection { return Selection{Mode: ModeFirstN, Limit: 10} }, ErrFilterRequired},

		{"everyone without a count", func() Selection { s := everyone; s.ExpectedCount = 0; return s }, ErrEveryoneUnconfirmed},
		{"everyone with a filter", func() Selection {
			s := everyone
			s.Filter = stageFilter("s1")
			return s
		}, ErrAmbiguousSelection},
		{"everyone with ids", func() Selection { s := everyone; s.IDs = []string{"a"}; return s }, ErrAmbiguousSelection},
		{"everyone without fingerprint", func() Selection { s := everyone; s.Fingerprint = ""; return s }, ErrFingerprintMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.mutate().Validate()
			if tc.want == nil {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestFingerprint(t *testing.T) {
	base := *stageFilter("s1")
	cases := []struct {
		name  string
		left  crmfilter.Filter
		right crmfilter.Filter
		same  bool
	}{
		{"same filter", base, *stageFilter("s1"), true},
		{"different value", base, *stageFilter("s2"), false},
		{"nil and empty groups", crmfilter.Filter{}, crmfilter.Filter{Groups: []crmfilter.Group{}}, true},
		{"empty group dropped", crmfilter.Filter{}, crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And}}}, true},
		{"missing conjunction reads as or", crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: base.Groups[0].Predicates}}},
			crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.Or, Predicates: base.Groups[0].Predicates}}}, true},
		{"and differs from or", base, crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.Or, Predicates: base.Groups[0].Predicates}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Fingerprint(tc.left) == Fingerprint(tc.right); got != tc.same {
				t.Fatalf("same = %v, want %v", got, tc.same)
			}
		})
	}
	if fp := Fingerprint(base); len(fp) != 64 || strings.Trim(fp, "0123456789abcdef") != "" {
		t.Fatalf("fingerprint must be a sha256 hex digest, got %q", fp)
	}
}

func TestEffectiveFilter(t *testing.T) {
	cases := []struct {
		name  string
		sel   Selection
		empty bool
	}{
		{"everyone reads the whole base", Selection{Mode: ModeEveryone}, true},
		{"all matching reads its filter", matching(stageFilter("s1")), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.sel.EffectiveFilter().IsEmpty(); got != tc.empty {
				t.Fatalf("empty = %v, want %v", got, tc.empty)
			}
		})
	}
}

func TestConfirmCount(t *testing.T) {
	cases := []struct {
		name    string
		sel     Selection
		matched int
		changed bool
	}{
		{"unconfirmed all matching takes the live count", matching(stageFilter("s1")), 300, false},
		{"confirmed count still holds", Selection{Mode: ModeAllMatching, ExpectedCount: 300}, 300, false},
		{"confirmed count moved", Selection{Mode: ModeAllMatching, ExpectedCount: 300}, 301, true},
		{"everyone moved", Selection{Mode: ModeEveryone, ExpectedCount: 10}, 9, true},
		{"first n caps the expected count", Selection{Mode: ModeFirstN, Limit: 100, ExpectedCount: 100}, 5000, false},
		{"first n below its limit", Selection{Mode: ModeFirstN, Limit: 100, ExpectedCount: 100}, 40, true},
		{"explicit ids are not recounted", Selection{Mode: ModeIDs, IDs: []string{"a"}, ExpectedCount: 4}, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.sel.ConfirmCount(tc.matched)
			if !tc.changed {
				if err != nil {
					t.Fatalf("want confirmed, got %v", err)
				}
				return
			}
			var changed *CountChangedError
			if !errors.As(err, &changed) || !errors.Is(err, ErrCountChanged) {
				t.Fatalf("want CountChangedError, got %v", err)
			}
			if changed.Matched != tc.matched || changed.Expected != tc.sel.ExpectedCount {
				t.Fatalf("unexpected counts %+v", changed)
			}
		})
	}
}

func TestResultFail(t *testing.T) {
	var r Result
	r.Fail("e1", errors.New("boom"))
	r.Skip("blocked")
	r.Skip("blocked")
	if len(r.Failed) != 1 || r.Failed[0].ID != "e1" || r.Failed[0].Error != "boom" {
		t.Fatalf("unexpected failures %+v", r.Failed)
	}
	if r.Skipped["blocked"] != 2 {
		t.Fatalf("unexpected skips %+v", r.Skipped)
	}
}

func TestValidatePage(t *testing.T) {
	cases := []struct {
		limit int
		ok    bool
	}{{0, false}, {-1, false}, {1, true}, {MaxExplicitIDs + 1, true}, {MaxResolvePage, true}, {MaxResolvePage + 1, false}}
	for _, tc := range cases {
		if err := ValidatePage(tc.limit); (err == nil) != tc.ok || (err != nil && !errors.Is(err, ErrInvalidPage)) {
			t.Errorf("limit %d: got %v", tc.limit, err)
		}
	}
}

func TestErrorCode(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{nil, ""},
		{ErrUnknownMode, "selection_unknown_mode"},
		{fmt.Errorf("wrapped: %w", ErrEmptySelection), "selection_empty"},
		{ErrAmbiguousSelection, "selection_ambiguous"},
		{ErrTooManyIDs, "selection_too_many_ids"},
		{ErrFilterRequired, "selection_filter_required"},
		{ErrLimitRequired, "selection_limit_required"},
		{ErrEveryoneUnconfirmed, "selection_everyone_unconfirmed"},
		{ErrFingerprintMismatch, "selection_fingerprint_mismatch"},
		{ErrInvalidCount, "selection_invalid_count"},
		{&CountChangedError{Expected: 1, Matched: 2}, "selection_changed"},
		{ErrModeUnsupported, "selection_mode_unsupported"},
		{ErrResolverUnavailable, "selection_unavailable"},
		{ErrInvalidPage, "selection_invalid_page"},
		{crmfilter.ErrUnknownField, "invalid_filter"},
		{crmfilter.ErrInvalidValue, "invalid_filter"},
		{crmfilter.ErrTooManyValues, "invalid_filter"},
		{crmfilter.ErrConjunctionRequired, "invalid_filter"},
		{crmfilter.ErrBirthdayClockMissing, ""},
		{errors.New("db down"), ""},
	}
	for _, tc := range cases {
		if got := ErrorCode(tc.err); got != tc.code {
			t.Errorf("ErrorCode(%v) = %q, want %q", tc.err, got, tc.code)
		}
	}
}
