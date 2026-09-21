package analytics

import "testing"

func TestAPeriodMetaHasNotAnsweredForIsStillAnInference(t *testing.T) {
	totals := MetaServiceMessageCostTotals{ServiceMessages: 319134, MetaAnswered: 0}
	if totals.FullyAnsweredByMeta() {
		t.Error("a period Meta has said nothing about cannot be fully answered")
	}
}

func TestPartialCoverageIsStillAnInference(t *testing.T) {
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

func TestCoverageBeyondTheTotalIsStillComplete(t *testing.T) {
	totals := MetaServiceMessageCostTotals{ServiceMessages: 100, MetaAnswered: 104}
	if !totals.FullyAnsweredByMeta() {
		t.Error("coverage above the counted total must not read as incomplete")
	}
}

func TestAnEmptyPeriodIsNeverFullyAnswered(t *testing.T) {
	totals := MetaServiceMessageCostTotals{ServiceMessages: 0, MetaAnswered: 0}
	if totals.FullyAnsweredByMeta() {
		t.Error("nothing counted is not the same as everything confirmed")
	}
}

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

func TestProviderNormalizationNeverWidens(t *testing.T) {
	if got := ServiceMessageProvider("nonsense").Normalized(); got != ServiceMessageProviderMeta {
		t.Errorf("unknown provider normalized to %q, want meta", got)
	}
	if got := ServiceMessageProviderAll.Normalized(); got != ServiceMessageProviderAll {
		t.Errorf("explicit all normalized to %q, want it preserved", got)
	}
	if got := NormalizeServiceMessageProvider(""); got != ServiceMessageProviderMeta {
		t.Errorf("absent provider parameter became %q, want meta", got)
	}
	if got := NormalizeServiceMessageProvider("all"); got != ServiceMessageProviderAll {
		t.Errorf("provider=all became %q, want all", got)
	}
}

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
