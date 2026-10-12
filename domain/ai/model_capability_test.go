package ai

import (
	"encoding/json"
	"slices"
	"testing"
)

func price(prompt, completion float64) *ModelPrice {
	return &ModelPrice{PromptPerMillion: prompt, CompletionPerMillion: completion}
}

func TestDescribeModel_DerivesEachCapabilityFromCatalogFacts(t *testing.T) {
	cases := []struct {
		name  string
		facts ModelFacts
		want  []ModelCapability
	}{
		{
			name:  "image input means the model sees images",
			facts: ModelFacts{InputModalities: []string{"text", "image"}},
			want:  []ModelCapability{CapabilityVision},
		},
		{
			name:  "tools parameter means tool use",
			facts: ModelFacts{Parameters: []string{"temperature", "tools"}},
			want:  []ModelCapability{CapabilityTools},
		},
		{
			name:  "reasoning parameter means reasoning",
			facts: ModelFacts{Parameters: []string{"reasoning", "include_reasoning"}},
			want:  []ModelCapability{CapabilityReasoning},
		},
		{
			name:  "a context of a million tokens is long context",
			facts: ModelFacts{ContextLength: LongContextTokens},
			want:  []ModelCapability{CapabilityLongContext},
		},
		{
			name:  "image output modality means image output",
			facts: ModelFacts{OutputModalities: []string{"image", "text"}},
			want:  []ModelCapability{CapabilityImageOutput},
		},
		{
			name:  "audio input modality means audio input",
			facts: ModelFacts{InputModalities: []string{"audio"}},
			want:  []ModelCapability{CapabilityAudioInput},
		},
		{
			name:  "known zero prices are free and low priced",
			facts: ModelFacts{Price: price(0, 0)},
			want:  []ModelCapability{CapabilityLowPrice, CapabilityFree},
		},
		{
			name:  "prices at the ceiling are low priced",
			facts: ModelFacts{Price: price(LowPromptPricePerMillion, LowCompletionPricePerMillion)},
			want:  []ModelCapability{CapabilityLowPrice},
		},
		{
			name:  "an output price above the ceiling is not low priced",
			facts: ModelFacts{Price: price(0.1, LowCompletionPricePerMillion+0.01)},
			want:  []ModelCapability{},
		},
		{
			name:  "a paid prompt with a free completion is not free",
			facts: ModelFacts{Price: price(0.1, 0)},
			want:  []ModelCapability{CapabilityLowPrice},
		},
		{
			name: "every capability at once keeps the vocabulary order",
			facts: ModelFacts{
				InputModalities:  []string{"text", "image", "audio"},
				OutputModalities: []string{"text", "image"},
				Parameters:       []string{"tools", "reasoning"},
				ContextLength:    2_000_000,
				Price:            price(0, 0),
			},
			want: ModelCapabilities(),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DescribeModel(tc.facts).Capabilities
			if !slices.Equal(got, tc.want) {
				t.Fatalf("capabilities = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDescribeModel_UnknownFactsMatchNoCapability(t *testing.T) {
	cases := []struct {
		name  string
		facts ModelFacts
	}{
		{name: "nothing known", facts: ModelFacts{}},
		{name: "unknown price is neither free nor low priced", facts: ModelFacts{Price: nil}},
		{name: "unknown modalities and parameters are ignored", facts: ModelFacts{InputModalities: []string{"hologram"}, OutputModalities: []string{"smell"}, Parameters: []string{"telepathy"}}},
		{name: "a context just under a million tokens is not long", facts: ModelFacts{ContextLength: LongContextTokens - 1}},
		{name: "negative prices are not known prices", facts: ModelFacts{Price: price(-1, -1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := DescribeModel(tc.facts)
			if info.Capabilities == nil || len(info.Capabilities) != 0 {
				t.Fatalf("capabilities = %#v, want an empty list", info.Capabilities)
			}
			if info.SeesImages() || info.HearsAudio() || info.Has(CapabilityTools) {
				t.Fatal("an unknown capability must not match")
			}
		})
	}
}

func TestDescribeModel_CarriesIdentityPricesAndContext(t *testing.T) {
	info := DescribeModel(ModelFacts{ID: "lab/model", Name: "Model", Created: 42, ContextLength: 128_000, Price: price(3, 15)})
	if info.ID != "lab/model" || info.Name != "Model" || info.Created != 42 || info.ContextLength != 128_000 {
		t.Fatalf("identity = %+v", info)
	}
	if info.PromptPrice != 3 || info.CompletionPrice != 15 {
		t.Fatalf("prices = %v / %v, want 3 / 15", info.PromptPrice, info.CompletionPrice)
	}
}

func TestModelInfo_JSONCarriesCapabilitiesAsAList(t *testing.T) {
	raw, err := json.Marshal(DescribeModel(ModelFacts{ID: "a"}))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if list, ok := decoded["capabilities"].([]any); !ok || len(list) != 0 {
		t.Fatalf("capabilities = %#v, want []", decoded["capabilities"])
	}
	if _, present := decoded["reasoning"]; present {
		t.Fatal("reasoning must be omitted when the model offers no levels")
	}
}

func TestFindModel(t *testing.T) {
	models := []ModelInfo{{ID: "a"}, {ID: "b", Name: "B"}}
	if m, ok := FindModel(models, "b"); !ok || m.Name != "B" {
		t.Fatalf("FindModel(b) = %+v, %v", m, ok)
	}
	if _, ok := FindModel(models, "c"); ok {
		t.Fatal("FindModel(c) found a model that is not in the catalog")
	}
}
