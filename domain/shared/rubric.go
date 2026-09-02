package shared

import (
	"fmt"
	"math"
	"strings"
)

// Rubric machinery shared by every AI classification in the system.
//
// A rubric is the SINGLE SOURCE OF TRUTH for what a model may answer: the set
// of valid labels per field, the criteria for each, and (for anything scored
// 0-100) the weighted ordinal dimensions the number is computed from. The
// tool schema, the JSON response schema, the prompt text and the persisted
// enum all render from the same table, so they cannot drift.
//
// This file owns the GENERIC half: the types, the renderers and the score
// arithmetic. Each domain (conversation analysis, comment analysis, …) owns
// its own taxonomy on top. The two were one file until a second rubric needed
// the same machinery; splitting them is what keeps the second rubric from
// re-implementing Description() and the weighted score and drifting.

// ---- Classification fields ----

// ClassificationOption is one allowed value and the criterion for choosing it.
type ClassificationOption struct {
	Value       string
	Description string
}

// ClassificationField is one labelled axis a model classifies on.
type ClassificationField struct {
	Key     string
	Title   string
	Intro   string
	Options []ClassificationOption
}

// Values returns the allowed enum values (for a tool or JSON schema's Enum).
func (f ClassificationField) Values() []string {
	out := make([]string, len(f.Options))
	for i, o := range f.Options {
		out[i] = o.Value
	}
	return out
}

// Description renders the field's criteria for a tool parameter description.
func (f ClassificationField) Description() string {
	var b strings.Builder
	b.WriteString(f.Intro)
	for _, o := range f.Options {
		fmt.Fprintf(&b, "\n- \"%s\": %s", o.Value, o.Description)
	}
	return b.String()
}

// RenderClassificationRubric renders the fields' criteria for a prompt: the
// same content a tool schema exposes per field, numbered and indented.
func RenderClassificationRubric(fields []ClassificationField) string {
	var b strings.Builder
	for i, f := range fields {
		fmt.Fprintf(&b, "%d. %s, %s\n", i+1, f.Key, f.Intro)
		for _, o := range f.Options {
			fmt.Fprintf(&b, "   - \"%s\": %s\n", o.Value, o.Description)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ---- Ordinal dimensions and the weighted 0-100 score ----
//
// A 0-100 score is NOT emitted by the model. Asking an LLM for an arbitrary
// 0-100 number is poorly calibrated (the same call scores differently run to
// run). Instead the model rates a few weighted dimensions on a coarse ordinal
// scale (which LLMs do far more consistently) and the numeric score is computed
// here, deterministically.

// QualityLevel is the ordinal rating the model assigns to each dimension.
type QualityLevel string

const (
	QualityLevelNone   QualityLevel = "none"
	QualityLevelLow    QualityLevel = "low"
	QualityLevelMedium QualityLevel = "medium"
	QualityLevelHigh   QualityLevel = "high"
)

func (l QualityLevel) Valid() bool {
	switch l {
	case QualityLevelNone, QualityLevelLow, QualityLevelMedium, QualityLevelHigh:
		return true
	}
	return false
}

// fraction maps an ordinal level onto [0,1].
func (l QualityLevel) fraction() float64 {
	switch l {
	case QualityLevelHigh:
		return 1.0
	case QualityLevelMedium:
		return 0.66
	case QualityLevelLow:
		return 0.33
	default: // none or invalid
		return 0.0
	}
}

func QualityLevelValues() []string {
	return []string{
		string(QualityLevelNone), string(QualityLevelLow),
		string(QualityLevelMedium), string(QualityLevelHigh),
	}
}

// QualityDimension is one weighted axis of a score. The weights are the ONLY
// definition of how the 0-100 score is composed.
type QualityDimension struct {
	Key         string
	Weight      float64
	Label       string
	Description string
}

// WeightedScore composes the 0-100 score from the dimensions' weights and the
// ordinal level rated for each (levelFor is asked by dimension key; an unknown
// or invalid level rates as none). The result is always within [0,100],
// regardless of the weights: it is a number customers read.
func WeightedScore(dims []QualityDimension, levelFor func(key string) QualityLevel) int {
	var total float64
	for _, d := range dims {
		total += d.Weight * levelFor(d.Key).fraction()
	}
	score := int(math.Round(total * 100))
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

// WeightsSumToOne reports whether a set of dimensions is a complete weighting.
// Rubric tests call it so a weight edit that forgets to rebalance fails fast.
func WeightsSumToOne(dims []QualityDimension) error {
	var sum float64
	for _, d := range dims {
		sum += d.Weight
	}
	if sum < 0.999 || sum > 1.001 {
		return fmt.Errorf("dimension weights sum to %v, want 1.0", sum)
	}
	return nil
}

// RenderQualityDimensions renders one prompt line per dimension: key, label,
// weight and criterion.
func RenderQualityDimensions(dims []QualityDimension) string {
	var b strings.Builder
	for _, d := range dims {
		fmt.Fprintf(&b, "- %s (%s, peso %.0f%%): %s\n", d.Key, d.Label, d.Weight*100, d.Description)
	}
	return b.String()
}
