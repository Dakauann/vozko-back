package emaildesign

import (
	"errors"
	"reflect"
	"testing"
)

func TestParseInlineReadsParagraphsMarksAndLinks(t *testing.T) {
	got, err := ParseInline("Olá, **{{first_name|cliente}}**!\nVeja *as* __novidades__.\n\n[Compre agora](https://loja.example.com/a?b=1) e ganhe ***frete***")
	if err != nil {
		t.Fatal(err)
	}
	want := []Paragraph{
		{Runs: []Run{
			{Text: "Olá, "},
			{Text: "{{first_name|cliente}}", Bold: true},
			{Text: "!\nVeja "},
			{Text: "as", Italic: true},
			{Text: " "},
			{Text: "novidades", Underline: true},
			{Text: "."},
		}},
		{Runs: []Run{
			{Text: "Compre agora", Link: &Link{Href: "https://loja.example.com/a?b=1"}},
			{Text: " e ganhe "},
			{Text: "frete", Bold: true, Italic: true},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParseInlineKeepsEscapesAndSingleUnderscores(t *testing.T) {
	got, err := ParseInline(`2 \* 3 = 6, snake_case, \[nota\], \\ e \__dois`)
	if err != nil {
		t.Fatal(err)
	}
	want := []Paragraph{{Runs: []Run{{Text: `2 * 3 = 6, snake_case, [nota], \ e __dois`}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestParseInlineTogglesAdjacentMarksWithoutAmbiguity(t *testing.T) {
	got, err := ParseInline("*a***b**")
	if err != nil {
		t.Fatal(err)
	}
	want := []Paragraph{{Runs: []Run{{Text: "a", Italic: true}, {Text: "b", Bold: true}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestParseInlineRefusesBrokenMarkup(t *testing.T) {
	cases := map[string]string{
		"unclosed bold":   "**oi",
		"unclosed link":   "[oi](https://x.com",
		"label only":      "[oi] solto",
		"too many stars":  "****oi****",
		"unclosed italic": "oi *mundo",
		"empty":           "  \n\n ",
		"nested link":     "[a [b](https://x.com)](https://y.com)",
	}
	for name, markup := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseInline(markup); !errors.Is(err, ErrInlineMarkup) {
				t.Fatalf("expected ErrInlineMarkup, got %v", err)
			}
		})
	}
}

func TestFormatInlineRoundTrips(t *testing.T) {
	paragraphs := []Paragraph{
		{Runs: []Run{
			{Text: "Preço: 2 * 3 [promo] a__b snake_case \\ "},
			{Text: "negrito", Bold: true},
			{Text: "itálico", Italic: true},
			{Text: "ambos", Bold: true, Italic: true},
			{Text: "sublinhado", Underline: true},
			{Text: "{{first_name|cliente}}", Bold: true, Link: &Link{Href: "https://x.com/p(1)"}},
			{Text: " fim"},
		}},
		{Runs: []Run{{Text: "linha 1\nlinha 2"}}},
	}
	markup := FormatInline(paragraphs)
	back, err := ParseInline(markup)
	if err != nil {
		t.Fatalf("%q: %v", markup, err)
	}
	want := []Paragraph{
		paragraphs[0],
		paragraphs[1],
	}
	want[0].Runs[5].Link = &Link{Href: "https://x.com/p(1%29"}
	if !reflect.DeepEqual(back, want) {
		t.Fatalf("markup %q\ngot  %+v\nwant %+v", markup, back, want)
	}
}

func TestFormatInlineMergesEqualNeighbours(t *testing.T) {
	got := FormatInline([]Paragraph{{Runs: []Run{{Text: "a", Bold: true}, {Text: "b", Bold: true}, {Text: "c"}}}})
	if got != "**ab**c" {
		t.Fatalf("got %q", got)
	}
}
