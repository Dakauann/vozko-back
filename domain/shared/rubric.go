package shared

import (
	"fmt"
	"math"
	"strings"
)

type ClassificationOption struct {
	Value       string
	Description string
}

type ClassificationField struct {
	Key     string
	Title   string
	Intro   string
	Options []ClassificationOption
}

func (f ClassificationField) Values() []string {
	out := make([]string, len(f.Options))
	for i, o := range f.Options {
		out[i] = o.Value
	}
	return out
}

func (f ClassificationField) Description() string {
	var b strings.Builder
	b.WriteString(f.Intro)
	for _, o := range f.Options {
		fmt.Fprintf(&b, "\n- \"%s\": %s", o.Value, o.Description)
	}
	return b.String()
}

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

func (l QualityLevel) fraction() float64 {
	switch l {
	case QualityLevelHigh:
		return 1.0
	case QualityLevelMedium:
		return 0.66
	case QualityLevelLow:
		return 0.33
	default:
		return 0.0
	}
}

func QualityLevelValues() []string {
	return []string{
		string(QualityLevelNone), string(QualityLevelLow),
		string(QualityLevelMedium), string(QualityLevelHigh),
	}
}

type QualityDimension struct {
	Key         string
	Weight      float64
	Label       string
	Description string
}

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

func RenderQualityDimensions(dims []QualityDimension) string {
	var b strings.Builder
	for _, d := range dims {
		fmt.Fprintf(&b, "- %s (%s, peso %.0f%%): %s\n", d.Key, d.Label, d.Weight*100, d.Description)
	}
	return b.String()
}
