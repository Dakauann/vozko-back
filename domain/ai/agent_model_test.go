package ai

import (
	"context"
	"errors"
	"testing"
)

type stubCatalog struct {
	models []ModelInfo
	err    error
}

func (c stubCatalog) GetModelsWithPricing(context.Context) ([]ModelInfo, error) {
	return c.models, c.err
}

func agentThinker() ModelInfo {
	return DescribeModel(reasoningModel([]string{"low", "medium", "high"}, "medium", false))
}

func TestResolveAgentModel_RefusesWhatItCannotConfirm(t *testing.T) {
	blind := DescribeModel(ModelFacts{ID: "lab/thinker", InputModalities: []string{"image"}})
	cases := []struct {
		name    string
		catalog ModelCatalog
		level   ReasoningLevel
		want    error
	}{
		{name: "the catalog cannot be read", catalog: stubCatalog{err: ErrProviderUnavailable}, want: ErrModelCatalogUnavailable},
		{name: "there is no catalog", catalog: nil, want: ErrModelCatalogUnavailable},
		{name: "the catalog is empty", catalog: stubCatalog{models: []ModelInfo{}}, want: ErrModelWithoutTools},
		{name: "the model is not listed", catalog: stubCatalog{models: []ModelInfo{{ID: "other", Capabilities: []ModelCapability{CapabilityTools}}}}, want: ErrModelWithoutTools},
		{name: "the model has no tools", catalog: stubCatalog{models: []ModelInfo{blind}}, want: ErrModelWithoutTools},
		{name: "the level is not offered", catalog: stubCatalog{models: []ModelInfo{agentThinker()}}, level: ReasoningMax, want: ErrReasoningLevelUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveAgentModel(context.Background(), tc.catalog, "lab/thinker", tc.level)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestResolveAgentModel_ReturnsTheConfirmedModel(t *testing.T) {
	for _, level := range []ReasoningLevel{"", ReasoningHigh} {
		got, err := ResolveAgentModel(context.Background(), stubCatalog{models: []ModelInfo{agentThinker()}}, "lab/thinker", level)
		if err != nil {
			t.Fatalf("level %q: err = %v", level, err)
		}
		if got.ID != "lab/thinker" || !got.Has(CapabilityTools) {
			t.Fatalf("level %q: model = %+v, want the catalog entry", level, got)
		}
	}
}

func TestRefusalOf_GivesEachRuleItsOwnCode(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{err: errors.Join(ErrModelCatalogUnavailable, ErrProviderUnavailable), code: RefusalModelCatalogUnavailable},
		{err: ErrModelWithoutTools, code: RefusalModelWithoutTools},
		{err: ErrReasoningLevelUnsupported, code: RefusalReasoningUnsupported},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		got, ok := RefusalOf(tc.err)
		if !ok || got.Code != tc.code || got.Message == "" {
			t.Fatalf("RefusalOf(%v) = %+v, %v, want code %q with a message", tc.err, got, ok, tc.code)
		}
		if seen[got.Message] {
			t.Fatalf("message %q is shared by two refusals", got.Message)
		}
		seen[got.Message] = true
	}
	if _, ok := RefusalOf(errors.New("something else")); ok {
		t.Fatal("an unrelated error must not become a model refusal")
	}
	if _, ok := RefusalOf(nil); ok {
		t.Fatal("no error must not become a model refusal")
	}
}
