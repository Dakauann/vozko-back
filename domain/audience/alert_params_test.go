package audience

import (
	"strings"
	"testing"
)

// The facts an alert can put into a template, and how they survive contact
// with a template that declares a different number of them, or names them.
//
// Every case here is a real way a customer's approved template can be shaped.
// WhatsApp rejects an empty parameter and a parameter count that disagrees with
// the template, and a rejection means an alert that silently did not arrive.

// The advertised count and the actual fact list must agree: the settings
// screen shows one and the send fills the other.
func TestAlertTemplateParamCountMatchesTheFacts(t *testing.T) {
	if AlertTemplateParamCount != len(AlertFactKeys()) {
		t.Fatalf("AlertTemplateParamCount = %d, AlertFactKeys has %d", AlertTemplateParamCount, len(AlertFactKeys()))
	}
	facts := NewAlert(validRule(), alertObservation(), alertNow()).Facts()
	if len(facts) != len(AlertFactKeys()) {
		t.Fatalf("Facts returned %d, AlertFactKeys has %d", len(facts), len(AlertFactKeys()))
	}
}

func TestAlertFactsAreNamedAndNonEmpty(t *testing.T) {
	facts := NewAlert(validRule(), alertObservation(), alertNow()).Facts()
	if len(facts) == 0 {
		t.Fatal("an alert with no facts has nothing to say")
	}
	seen := map[string]bool{}
	for _, f := range facts {
		if strings.TrimSpace(f.Key) == "" {
			t.Fatalf("a fact with no key cannot be matched to a named parameter: %+v", f)
		}
		if strings.TrimSpace(f.Value) == "" {
			t.Fatalf("fact %q is empty, which WhatsApp rejects", f.Key)
		}
		if seen[f.Key] {
			t.Fatalf("duplicate fact key %q", f.Key)
		}
		seen[f.Key] = true
	}
}

// A template with no variables is legal and common: "você tem um alerta".
func TestTemplateParamsForNoVariables(t *testing.T) {
	alert := NewAlert(validRule(), alertObservation(), alertNow())
	if got := alert.TemplateParamsFor([]string{}); len(got) != 0 {
		t.Fatalf("params = %v, want none", got)
	}
}

// A POSITIONAL template takes the facts in their canonical order, however many
// it declares.
func TestTemplateParamsForPositional(t *testing.T) {
	alert := NewAlert(validRule(), alertObservation(), alertNow())

	two := alert.TemplateParamsFor([]string{"1", "2"})
	if len(two) != 2 {
		t.Fatalf("params = %v, want 2", two)
	}
	if two[0] != validRule().Name {
		t.Fatalf("the first parameter should be the rule name, got %q", two[0])
	}

	// More parameters than facts: padded rather than left empty, because an
	// empty parameter is refused by the provider.
	many := alert.TemplateParamsFor([]string{"1", "2", "3", "4", "5", "6", "7", "8"})
	if len(many) != 8 {
		t.Fatalf("params = %d, want 8", len(many))
	}
	for i, p := range many {
		if strings.TrimSpace(p) == "" {
			t.Fatalf("param %d is empty: %v", i, many)
		}
	}
}

// A NAMED template gets its facts matched by name, so "excerpt" receives the
// comment even when the template lists it first.
func TestTemplateParamsForNamedMatchesByName(t *testing.T) {
	alert := NewAlert(validRule(), alertObservation(), alertNow())
	got := alert.TemplateParamsFor([]string{"excerpt", "rule"})

	if len(got) != 2 {
		t.Fatalf("params = %v", got)
	}
	if !strings.Contains(got[0], "ladrões") {
		t.Fatalf("the excerpt parameter got %q", got[0])
	}
	if got[1] != validRule().Name {
		t.Fatalf("the rule parameter got %q", got[1])
	}
}

// Name matching is forgiving about the shapes people actually name variables:
// case, underscores and the {{ }} the editor sometimes leaves in.
func TestTemplateParamsForNamedIsForgiving(t *testing.T) {
	alert := NewAlert(validRule(), alertObservation(), alertNow())
	for _, name := range []string{"Excerpt", "EXCERPT", "{{excerpt}}", " excerpt "} {
		got := alert.TemplateParamsFor([]string{name})
		if !strings.Contains(got[0], "ladrões") {
			t.Fatalf("%q did not match the excerpt fact, got %q", name, got[0])
		}
	}
}

// A named parameter we do not recognise still has to receive something, in the
// canonical order, rather than an empty string.
func TestTemplateParamsForUnknownNameFallsBackPositionally(t *testing.T) {
	alert := NewAlert(validRule(), alertObservation(), alertNow())
	got := alert.TemplateParamsFor([]string{"customer_name", "order_id"})

	if len(got) != 2 {
		t.Fatalf("params = %v", got)
	}
	for i, p := range got {
		if strings.TrimSpace(p) == "" {
			t.Fatalf("param %d is empty: %v", i, got)
		}
	}
	// The unmatched names are filled from the canonical order, so the first
	// still carries the rule name.
	if got[0] != validRule().Name {
		t.Fatalf("first fallback param = %q", got[0])
	}
}

// A named parameter that IS matched must not also be consumed by the
// positional fallback, or two parameters carry the same fact.
func TestTemplateParamsForMixedNamesDoNotRepeat(t *testing.T) {
	alert := NewAlert(validRule(), alertObservation(), alertNow())
	got := alert.TemplateParamsFor([]string{"rule", "whatever", "measurement"})

	if got[0] != validRule().Name {
		t.Fatalf("named 'rule' = %q", got[0])
	}
	if got[2] == got[0] {
		t.Fatalf("named 'measurement' repeated the rule name: %v", got)
	}
	if got[1] == got[0] || got[1] == got[2] {
		t.Fatalf("the fallback repeated a fact already used: %v", got)
	}
}

// Every parameter is flattened: a newline inside one is rejected by WhatsApp,
// and a comment can very much contain newlines.
func TestTemplateParamsForFlattensControlCharacters(t *testing.T) {
	obs := alertObservation()
	obs.Comment.Excerpt = "primeira linha\nsegunda\tlinha"
	got := NewAlert(validRule(), obs, alertNow()).TemplateParamsFor([]string{"excerpt"})

	if strings.ContainsAny(got[0], "\n\t") {
		t.Fatalf("param carries a control character: %q", got[0])
	}
	if !strings.Contains(got[0], "primeira linha") {
		t.Fatalf("flattening lost the text: %q", got[0])
	}
}

// A windowed alert has no comment, and every parameter still has to be filled.
func TestTemplateParamsForWindowedAlert(t *testing.T) {
	r := validRule()
	r.Metric = AlertMetricHostileCount
	r.Threshold = 10
	r.Normalize()

	got := NewAlert(r, AlertObservation{Metric: AlertMetricHostileCount, Value: 14}, alertNow()).
		TemplateParamsFor([]string{"rule", "measurement", "where", "excerpt"})
	for i, p := range got {
		if strings.TrimSpace(p) == "" {
			t.Fatalf("param %d is empty: %v", i, got)
		}
	}
}

// The legacy fixed-shape accessor keeps working and agrees with the general
// one, so the two cannot drift.
func TestTemplateParamsMatchesTheGeneralForm(t *testing.T) {
	alert := NewAlert(validRule(), alertObservation(), alertNow())
	fixed := alert.TemplateParams()
	general := alert.TemplateParamsFor(nil)

	if len(fixed) != len(general) {
		t.Fatalf("%d vs %d", len(fixed), len(general))
	}
	for i := range fixed {
		if fixed[i] != general[i] {
			t.Fatalf("param %d: %q vs %q", i, fixed[i], general[i])
		}
	}
}
