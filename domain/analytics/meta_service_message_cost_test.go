package analytics

import "testing"

// The report starts as our own reading of the message log and is supposed to
// become Meta's truth as the pricing columns fill. That only happens if the
// "this is an estimate" flag is derived from how much Meta has actually
// answered for, rather than asserted.
//
// It was asserted, once, and the consequence was a page that would have carried
// an upper bound caveat forever. These pin the derivation so it cannot go back.

func TestAPeriodMetaHasNotAnsweredForIsStillAnInference(t *testing.T) {
	totals := MetaServiceMessageCostTotals{ServiceMessages: 319134, MetaAnswered: 0}
	if totals.FullyAnsweredByMeta() {
		t.Error("a period Meta has said nothing about cannot be fully answered")
	}
}

func TestPartialCoverageIsStillAnInference(t *testing.T) {
	// One message short is still short. Rounding this to "close enough" would
	// drop the caveat while part of the figure was still a guess.
	totals := MetaServiceMessageCostTotals{ServiceMessages: 100, MetaAnswered: 99}
	if totals.FullyAnsweredByMeta() {
		t.Error("99 of 100 answered must still count as an inference")
	}
}

func TestFullCoverageStopsBeingAnInference(t *testing.T) {
	totals := MetaServiceMessageCostTotals{ServiceMessages: 100, MetaAnswered: 100}
	if !totals.FullyAnsweredByMeta() {
		t.Error("Meta answered for every message; the report is no longer our guess")
	}
}

// Coverage can exceed the counted total: a message can be answered for and then
// excluded from the count by a later status, for instance when the final status
// is failed. More than complete is complete.
func TestCoverageBeyondTheTotalIsStillComplete(t *testing.T) {
	totals := MetaServiceMessageCostTotals{ServiceMessages: 100, MetaAnswered: 104}
	if !totals.FullyAnsweredByMeta() {
		t.Error("coverage above the counted total must not read as incomplete")
	}
}

// An empty period has no confirmation to have. Claiming it is Meta-confirmed
// would let a page with nothing on it present itself as authoritative.
func TestAnEmptyPeriodIsNeverFullyAnswered(t *testing.T) {
	totals := MetaServiceMessageCostTotals{ServiceMessages: 0, MetaAnswered: 0}
	if totals.FullyAnsweredByMeta() {
		t.Error("nothing counted is not the same as everything confirmed")
	}
}

// Answered is not confirmed. A message Meta told us was free inside the 72 hour
// entry point is answered and not confirmed, and that gap is precisely the
// overcount our own rule cannot see, so the two must never be conflated.
func TestAnsweredAndConfirmedAreDifferentQuestions(t *testing.T) {
	totals := MetaServiceMessageCostTotals{
		ServiceMessages: 100,
		MetaAnswered:    100,
		MetaConfirmed:   80,
	}
	if !totals.FullyAnsweredByMeta() {
		t.Error("full coverage is about answers, not about how many were billable")
	}
	if totals.MetaConfirmed >= totals.ServiceMessages {
		t.Error("this fixture is meant to have 20 answered-but-not-billable messages")
	}
}

// The ratio is nil, never zero, when nothing was bought. Zero would read as
// "sends nothing per send", the opposite of the truth.
func TestRatioIsAbsentRatherThanZeroWithNoSends(t *testing.T) {
	if got := ComputeRatio(900, 0); got != nil {
		t.Errorf("ComputeRatio(900, 0) = %v, want nil", *got)
	}
	if got := ComputeRatio(0, 0); got != nil {
		t.Errorf("ComputeRatio(0, 0) = %v, want nil", *got)
	}
	got := ComputeRatio(13288, 8414)
	if got == nil || *got < 1.57 || *got > 1.58 {
		t.Errorf("ComputeRatio(13288, 8414) = %v, want about 1,58", got)
	}
}

// An unrecognised provider must narrow to Meta rather than widen to everything:
// widening would report numbers somebody else pays for as our own exposure.
func TestProviderNormalizationNeverWidens(t *testing.T) {
	if got := ServiceMessageProvider("nonsense").Normalized(); got != ServiceMessageProviderMeta {
		t.Errorf("unknown provider normalized to %q, want meta", got)
	}
	// "All" is a decision spelled with the empty string, and it has to survive.
	if got := ServiceMessageProviderAll.Normalized(); got != ServiceMessageProviderAll {
		t.Errorf("explicit all normalized to %q, want it preserved", got)
	}
	// An ABSENT query parameter is also the empty string, and there it means
	// "not stated", which must default to meta. The two readings are why
	// Normalized and NormalizeServiceMessageProvider cannot be the same
	// function.
	if got := NormalizeServiceMessageProvider(""); got != ServiceMessageProviderMeta {
		t.Errorf("absent provider parameter became %q, want meta", got)
	}
	if got := NormalizeServiceMessageProvider("all"); got != ServiceMessageProviderAll {
		t.Errorf("provider=all became %q, want all", got)
	}
}

// The sort field is interpolated into SQL by the repository, so anything off
// the whitelist has to be replaced before it gets there.
func TestUnknownSortFieldsFallBackToRatio(t *testing.T) {
	for _, raw := range []string{"", "nonsense", "1; DROP TABLE workspaces", "ratio"} {
		if got := NormalizeMetaServiceMessageCostSortField(raw); got != SortMetaServiceMessageCostRatio {
			if raw == "ratio" || raw == "" || raw == "nonsense" || raw == "1; DROP TABLE workspaces" {
				t.Errorf("NormalizeMetaServiceMessageCostSortField(%q) = %q, want ratio", raw, got)
			}
		}
	}
	if got := NormalizeMetaServiceMessageCostSortField("serviceMessages"); got != SortMetaServiceMessageCostServiceMessages {
		t.Errorf("a known field was replaced: got %q", got)
	}
}
