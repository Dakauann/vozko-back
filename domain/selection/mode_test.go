package selection

import (
	"errors"
	"fmt"
	"testing"

	"vozko/domain/crmfilter"
)

func TestModeFor(t *testing.T) {
	cases := []struct {
		name   string
		picked bool
		filter *crmfilter.Filter
		want   Mode
	}{
		{"picks are ids", true, nil, ModeIDs},
		{"picks win over a filter, which Validate then refuses as ambiguous", true, stageFilter("s1"), ModeIDs},
		{"a non-empty filter is all matching", false, stageFilter("s1"), ModeAllMatching},
		{"an empty filter is everyone", false, &crmfilter.Filter{}, ModeEveryone},
		{"a filter of empty groups is everyone", false, &crmfilter.Filter{Groups: []crmfilter.Group{{}}}, ModeEveryone},
		{"nothing at all names no mode", false, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ModeFor(tc.picked, tc.filter); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestInferred(t *testing.T) {
	explicit := Selection{Mode: ModeEveryone, IDs: []string{"a"}}
	if got := explicit.Inferred(); got.Mode != ModeEveryone {
		t.Fatalf("an explicit mode is kept, got %q", got.Mode)
	}
	if err := explicit.Inferred().Validate(); !errors.Is(err, ErrAmbiguousSelection) {
		t.Fatalf("picks sent with everyone must be refused, got %v", err)
	}

	legacy := Selection{Filter: stageFilter("s1")}.Inferred()
	if legacy.Mode != ModeAllMatching || legacy.Fingerprint != "" {
		t.Fatalf("inference never invents a fingerprint, got %+v", legacy)
	}
	if err := legacy.Validate(); !errors.Is(err, ErrFingerprintMismatch) {
		t.Fatalf("a filter without the counted fingerprint is refused, got %v", err)
	}
	if got := (Selection{IDs: []string{"a"}}).Inferred(); got.Mode != ModeIDs || got.Validate() != nil {
		t.Fatalf("picks infer ids, got %+v", got)
	}
}

func TestForFilter(t *testing.T) {
	cases := []struct {
		name   string
		filter crmfilter.Filter
		want   Mode
	}{
		{"a filter counts all matching", *stageFilter("s1"), ModeAllMatching},
		{"no filter counts everyone", crmfilter.Filter{}, ModeEveryone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counted := ForFilter(tc.filter)
			if counted.Mode != tc.want || counted.Fingerprint != Fingerprint(tc.filter) {
				t.Fatalf("got %+v", counted)
			}
			confirmed := counted
			confirmed.ExpectedCount = 3
			if err := confirmed.Validate(); err != nil {
				t.Fatalf("the counted selection, confirmed, must validate: %v", err)
			}
		})
	}
}

func TestBeforeExclusions(t *testing.T) {
	sel := matching(stageFilter("s1"))
	sel.ExcludeIDs = []string{"a", "b"}
	sel.ExpectedCount = 9

	counted := sel.BeforeExclusions()

	if counted.ExcludeIDs != nil || counted.ExpectedCount != 9 || counted.Fingerprint != sel.Fingerprint || counted.Mode != sel.Mode {
		t.Fatalf("only the exclusions are dropped, got %+v", counted)
	}
	if len(sel.ExcludeIDs) != 2 {
		t.Fatal("the original selection keeps its exclusions")
	}
}

func TestErrorCode_RefusalsFromTheResolver(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("resolve: %w", ErrScopeDenied), "selection_scope_denied"},
		{fmt.Errorf("compile: %w", crmfilter.ErrNotApplicable), CodeInvalidFilter},
	}
	for _, tc := range cases {
		if got := ErrorCode(tc.err); got != tc.want {
			t.Fatalf("%v: want %q, got %q", tc.err, tc.want, got)
		}
	}
}
