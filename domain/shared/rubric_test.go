package shared

import (
	"reflect"
	"strings"
	"testing"
)

func sampleField() ClassificationField {
	return ClassificationField{
		Key:   "mood",
		Title: "Humor",
		Intro: "Tom predominante:",
		Options: []ClassificationOption{
			{"up", "animado"},
			{"flat", "sem emoção"},
			{"down", "irritado"},
		},
	}
}

func TestClassificationField_Values(t *testing.T) {
	got := sampleField().Values()
	want := []string{"up", "flat", "down"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Values() = %v, want %v", got, want)
	}
}

// The tool-parameter description is the intro followed by one criterion per
// line. Pinned byte-for-byte: the conversation_analysis tool schema was
// rendering from this before the move, and its output must not change.
func TestClassificationField_Description(t *testing.T) {
	got := sampleField().Description()
	want := "Tom predominante:\n- \"up\": animado\n- \"flat\": sem emoção\n- \"down\": irritado"
	if got != want {
		t.Fatalf("Description() = %q, want %q", got, want)
	}
}

// The prompt renderer numbers the fields and indents the criteria. Same
// byte-for-byte guarantee as Description: the analysis prompts render through
// this and a drift here is a drift in every prompt.
func TestRenderClassificationRubric(t *testing.T) {
	fields := []ClassificationField{
		sampleField(),
		{Key: "size", Intro: "Tamanho:", Options: []ClassificationOption{{"s", "pequeno"}}},
	}
	got := RenderClassificationRubric(fields)
	want := "1. mood, Tom predominante:\n" +
		"   - \"up\": animado\n" +
		"   - \"flat\": sem emoção\n" +
		"   - \"down\": irritado\n" +
		"\n" +
		"2. size, Tamanho:\n" +
		"   - \"s\": pequeno\n" +
		"\n"
	if got != want {
		t.Fatalf("RenderClassificationRubric() =\n%q\nwant\n%q", got, want)
	}
}

func TestQualityLevel_Valid(t *testing.T) {
	for _, v := range QualityLevelValues() {
		if !QualityLevel(v).Valid() {
			t.Errorf("%q should be valid", v)
		}
	}
	for _, v := range []QualityLevel{"bogus", "", "High", " high"} {
		if v.Valid() {
			t.Errorf("%q should be invalid", v)
		}
	}
}

func TestQualityLevelValues_Order(t *testing.T) {
	want := []string{"none", "low", "medium", "high"}
	if got := QualityLevelValues(); !reflect.DeepEqual(got, want) {
		t.Fatalf("QualityLevelValues() = %v, want %v", got, want)
	}
}

// Two dimensions with 60/40 weights. The table below is the contract every
// rubric built on this machinery inherits: the numbers here are the numbers
// the attendance-quality tests were already asserting before the move.
func twoDims() []QualityDimension {
	return []QualityDimension{
		{Key: "a", Weight: 0.60},
		{Key: "b", Weight: 0.40},
	}
}

func levels(m map[string]QualityLevel) func(string) QualityLevel {
	return func(k string) QualityLevel { return m[k] }
}

func TestWeightedScore(t *testing.T) {
	cases := []struct {
		name string
		lv   map[string]QualityLevel
		want int
	}{
		{"all high = 100", map[string]QualityLevel{"a": QualityLevelHigh, "b": QualityLevelHigh}, 100},
		{"all none = 0", map[string]QualityLevel{"a": QualityLevelNone, "b": QualityLevelNone}, 0},
		{"all medium = 66", map[string]QualityLevel{"a": QualityLevelMedium, "b": QualityLevelMedium}, 66},
		{"a high only = 60", map[string]QualityLevel{"a": QualityLevelHigh}, 60},
		{"b high only = 40", map[string]QualityLevel{"b": QualityLevelHigh}, 40},
		// 0.6*0.33 + 0.4*0.66 = 0.462 -> 46
		{"a low, b medium = 46", map[string]QualityLevel{"a": QualityLevelLow, "b": QualityLevelMedium}, 46},
		// An unknown level rates as none rather than poisoning the score.
		{"garbage rates as none", map[string]QualityLevel{"a": "garbage", "b": QualityLevelHigh}, 40},
		{"missing key rates as none", map[string]QualityLevel{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WeightedScore(twoDims(), levels(tc.lv)); got != tc.want {
				t.Errorf("WeightedScore() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestWeightedScore_AlwaysInRange(t *testing.T) {
	all := []QualityLevel{QualityLevelNone, QualityLevelLow, QualityLevelMedium, QualityLevelHigh, "garbage"}
	for _, a := range all {
		for _, b := range all {
			s := WeightedScore(twoDims(), levels(map[string]QualityLevel{"a": a, "b": b}))
			if s < 0 || s > 100 {
				t.Fatalf("WeightedScore() = %d out of [0,100] for %q/%q", s, a, b)
			}
		}
	}
	// Even a mis-weighted rubric cannot escape the range: the score is a
	// number customers read, so the bound is unconditional.
	over := []QualityDimension{{Key: "a", Weight: 1.5}}
	if s := WeightedScore(over, levels(map[string]QualityLevel{"a": QualityLevelHigh})); s != 100 {
		t.Fatalf("over-weighted rubric scored %d, want clamped to 100", s)
	}
}

func TestWeightedScore_NoDimensions(t *testing.T) {
	if s := WeightedScore(nil, levels(nil)); s != 0 {
		t.Fatalf("WeightedScore(nil) = %d, want 0", s)
	}
}

func TestWeightsSumToOne(t *testing.T) {
	if err := WeightsSumToOne(twoDims()); err != nil {
		t.Fatalf("60/40 should sum to one: %v", err)
	}
	bad := []QualityDimension{{Key: "a", Weight: 0.5}, {Key: "b", Weight: 0.3}}
	if err := WeightsSumToOne(bad); err == nil {
		t.Fatal("0.8 total should be rejected")
	}
}

// One line per dimension, key + label + weight + description. The analysis
// prompt and the tool schema both render dimensions through this.
func TestRenderQualityDimensions(t *testing.T) {
	dims := []QualityDimension{
		{Key: "a", Weight: 0.60, Label: "Alfa", Description: "primeira"},
		{Key: "b", Weight: 0.40, Label: "Beta", Description: "segunda"},
	}
	got := RenderQualityDimensions(dims)
	want := "- a (Alfa, peso 60%): primeira\n- b (Beta, peso 40%): segunda\n"
	if got != want {
		t.Fatalf("RenderQualityDimensions() = %q, want %q", got, want)
	}
	if strings.Count(got, "\n") != len(dims) {
		t.Fatalf("expected exactly one line per dimension")
	}
}
