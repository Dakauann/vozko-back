package emaildesign

import (
	"math"
	"strings"
	"testing"
)

func findCheck(checks []Check, code, blockID string) (Check, bool) {
	for _, c := range checks {
		if c.Code == code && c.BlockID == blockID {
			return c, true
		}
	}
	return Check{}, false
}

func TestChecksPassAWellMadeEmail(t *testing.T) {
	doc := docWith(headingBlock("title", "Oi"), textBlock("body", Run{Text: "Texto", Link: &Link{Href: "https://x.com"}}), buttonBlock("cta", "Comprar", "https://x.com"), imageBlock("hero", "https://cdn.example.com/a.jpg", "Produto"))
	if checks := Checks(doc); len(checks) != 0 {
		t.Fatalf("got %+v", checks)
	}
}

func TestContrastRatioFollowsWCAG(t *testing.T) {
	if got := ContrastRatio("#000000", "#ffffff"); math.Abs(got-21) > 0.01 {
		t.Fatalf("got %v", got)
	}
	if got := ContrastRatio("#777777", "#ffffff"); math.Abs(got-4.48) > 0.01 {
		t.Fatalf("got %v", got)
	}
}

func TestChecksMeasureContrastAgainstTheRealBackground(t *testing.T) {
	pale := headingBlock("pale", "Título")
	pale.Heading.Color = "#aaaaaa"
	small := Block{ID: "small", Kind: KindHeading, Heading: &Heading{Text: "Sub", Level: 3, Color: "#8a8a8a"}}
	button := buttonBlock("cta", "Comprar", "https://x.com")
	button.Button.Color = "#ffd400"
	inside := textBlock("inside", Run{Text: "Oi"})
	cols := columnsBlock("cols", Ratio5050, []Block{inside}, nil)
	cols.BackgroundColor = "#333333"
	doc := docWith(pale, small, button, cols)
	checks := Checks(doc)
	for _, want := range []struct {
		id      string
		minimum float64
	}{{"pale", 3}, {"small", 4.5}, {"cta", 4.5}, {"inside", 4.5}} {
		c, ok := findCheck(checks, "low_contrast", want.id)
		if !ok || c.Minimum != want.minimum || c.Ratio <= 0 || c.Ratio >= want.minimum {
			t.Fatalf("%s: got %+v in %+v", want.id, c, checks)
		}
	}
}

func TestChecksReportStructureAndScanning(t *testing.T) {
	h3 := Block{ID: "deep", Kind: KindHeading, Heading: &Heading{Text: "Detalhe", Level: 3}}
	long := buttonBlock("b3", strings.Repeat("a", 31), "https://x.com")
	paragraph := textBlock("wall", Run{Text: strings.Repeat("palavra ", 70)})
	doc := docWith(headingBlock("top", "Oi"), h3, buttonBlock("b1", "Um", "https://x.com/1"), buttonBlock("b2", "Dois", "https://x.com/2"), buttonBlock("b4", "Quatro", "https://x.com/4"), long, paragraph)
	doc.Settings.Width = 700
	checks := Checks(doc)
	for _, want := range []struct{ code, id string }{
		{"heading_order", "deep"},
		{"many_buttons", ""},
		{"button_label_long", "b3"},
		{"long_paragraph", "wall"},
		{"width_wide", ""},
	} {
		if _, ok := findCheck(checks, want.code, want.id); !ok {
			t.Fatalf("missing %s on %q in %+v", want.code, want.id, checks)
		}
	}
}

func TestReviewJoinsHintsAndChecks(t *testing.T) {
	doc := docWith(imageBlock("hero", "http://x.com/a.jpg", ""))
	review := Review(doc)
	if !hasHint(review.Hints, "image_missing_alt", "hero") || review.Checks == nil {
		t.Fatalf("got %+v", review)
	}
}
