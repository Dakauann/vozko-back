package ai

import (
	"errors"
	"slices"
	"testing"
)

func reasoningModel(efforts []string, def string, mandatory bool) ModelFacts {
	return ModelFacts{
		ID:         "lab/thinker",
		Parameters: []string{"tools", "reasoning"},
		Reasoning:  &ReasoningFacts{Efforts: efforts, Default: def, Mandatory: mandatory},
	}
}

func TestDescribeModel_OffersTheCatalogEffortsInScaleOrder(t *testing.T) {
	info := DescribeModel(reasoningModel([]string{"max", "xhigh", "high", "medium", "low", "none"}, "medium", false))
	if info.Reasoning == nil {
		t.Fatal("reasoning options = nil, want the catalog efforts")
	}
	want := []ReasoningLevel{ReasoningNone, ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningXHigh, ReasoningMax}
	if !slices.Equal(info.Reasoning.Levels, want) {
		t.Fatalf("levels = %v, want %v", info.Reasoning.Levels, want)
	}
	if info.Reasoning.Default != ReasoningMedium {
		t.Fatalf("default = %q, want medium", info.Reasoning.Default)
	}
}

func TestDescribeModel_ReasoningLevelsFailClosed(t *testing.T) {
	cases := []struct {
		name  string
		facts ModelFacts
	}{
		{name: "no reasoning facts", facts: ModelFacts{Parameters: []string{"reasoning"}}},
		{name: "efforts omitted", facts: reasoningModel(nil, "medium", false)},
		{name: "reasoning parameter missing", facts: ModelFacts{Reasoning: &ReasoningFacts{Efforts: []string{"low", "high"}, Default: "low"}}},
		{name: "default missing", facts: reasoningModel([]string{"low", "high"}, "", false)},
		{name: "default not among the efforts", facts: reasoningModel([]string{"low", "high"}, "medium", false)},
		{name: "only unknown efforts", facts: reasoningModel([]string{"ultra", "turbo"}, "ultra", false)},
		{name: "mandatory reasoning that only offers none", facts: reasoningModel([]string{"none"}, "none", true)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DescribeModel(tc.facts).Reasoning; got != nil {
				t.Fatalf("reasoning = %+v, want nil", got)
			}
		})
	}
}

func TestDescribeModel_DropsUnknownEffortsAndDuplicates(t *testing.T) {
	info := DescribeModel(reasoningModel([]string{"high", "ultra", "low", "high"}, "low", false))
	if info.Reasoning == nil || !slices.Equal(info.Reasoning.Levels, []ReasoningLevel{ReasoningLow, ReasoningHigh}) {
		t.Fatalf("reasoning = %+v, want low and high only", info.Reasoning)
	}
}

func TestDescribeModel_MandatoryReasoningNeverOffersNone(t *testing.T) {
	info := DescribeModel(reasoningModel([]string{"high", "medium", "none"}, "medium", true))
	if info.Reasoning == nil || slices.Contains(info.Reasoning.Levels, ReasoningNone) {
		t.Fatalf("reasoning = %+v, want no none level", info.Reasoning)
	}
}

func TestModelInfo_AcceptsReasoning(t *testing.T) {
	thinker := DescribeModel(reasoningModel([]string{"high", "medium", "low"}, "medium", true))
	plain := DescribeModel(ModelFacts{ID: "lab/plain", Parameters: []string{"tools"}})
	cases := []struct {
		name  string
		model ModelInfo
		level ReasoningLevel
		err   error
	}{
		{name: "no level always passes", model: plain, level: ""},
		{name: "an offered level passes", model: thinker, level: ReasoningHigh},
		{name: "a level outside the offer is refused", model: thinker, level: ReasoningMax, err: ErrReasoningLevelUnsupported},
		{name: "none is refused when reasoning is mandatory", model: thinker, level: ReasoningNone, err: ErrReasoningLevelUnsupported},
		{name: "a model without levels refuses any level", model: plain, level: ReasoningLow, err: ErrReasoningLevelUnsupported},
		{name: "an unknown word is refused", model: thinker, level: ReasoningLevel("ultra"), err: ErrReasoningLevelUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.model.AcceptsReasoning(tc.level); !errors.Is(err, tc.err) {
				t.Fatalf("AcceptsReasoning(%q) = %v, want %v", tc.level, err, tc.err)
			}
		})
	}
}

func TestCheckReasoning_LooksTheModelUpInTheCatalog(t *testing.T) {
	catalog := []ModelInfo{DescribeModel(reasoningModel([]string{"low", "high"}, "low", false))}
	if err := CheckReasoning(catalog, "lab/thinker", ReasoningHigh); err != nil {
		t.Fatalf("offered level refused: %v", err)
	}
	if err := CheckReasoning(catalog, "lab/unknown", ReasoningHigh); !errors.Is(err, ErrReasoningLevelUnsupported) {
		t.Fatalf("unknown model err = %v, want refusal", err)
	}
	if err := CheckReasoning(nil, "lab/thinker", ReasoningHigh); !errors.Is(err, ErrReasoningLevelUnsupported) {
		t.Fatalf("empty catalog err = %v, want refusal", err)
	}
	if err := CheckReasoning(nil, "lab/unknown", ""); err != nil {
		t.Fatalf("no level err = %v, want nil", err)
	}
}

func TestParseReasoningLevel_TrimsAndKeepsTheWord(t *testing.T) {
	if got := ParseReasoningLevel("  high "); got != ReasoningHigh {
		t.Fatalf("ParseReasoningLevel = %q, want high", got)
	}
	if got := ParseReasoningLevel(""); got != "" {
		t.Fatalf("ParseReasoningLevel(empty) = %q, want empty", got)
	}
}
