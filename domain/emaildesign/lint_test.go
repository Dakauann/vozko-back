package emaildesign

import (
	"strconv"
	"strings"
	"testing"
)

func lintableDocument() Document {
	doc := docWith(
		headingBlock("h", "Novidades da semana"),
		textBlock("t", Run{Text: strings.Repeat("Conteudo relevante para quem assina. ", 6)}),
		buttonBlock("b", "Ver mais", "https://example.com"),
	)
	doc.Settings.Preheader = "As novidades que separamos"
	return doc
}

func TestLintCleanDocumentHasNoHints(t *testing.T) {
	if hints := Lint(lintableDocument(), "Novidades da semana para voce"); len(hints) != 0 {
		t.Fatalf("expected no hints, got %+v", hints)
	}
}

func TestLintHints(t *testing.T) {
	manyLinks := make([]Run, 0, 26)
	for i := 0; i < 26; i++ {
		manyLinks = append(manyLinks, Run{Text: "l" + strconv.Itoa(i), Link: &Link{Href: "https://example.com/" + strconv.Itoa(i)}})
	}
	cases := []struct {
		name    string
		doc     Document
		subject string
		code    string
		blockID string
	}{
		{name: "subject long", doc: lintableDocument(), subject: strings.Repeat("a", 61), code: "subject_too_long"},
		{name: "subject caps", doc: lintableDocument(), subject: "OFERTA IMPERDIVEL HOJE", code: "subject_all_caps"},
		{name: "subject caps with tag", doc: lintableDocument(), subject: "OFERTA PARA {{first_name}}", code: "subject_all_caps"},
		{name: "subject punctuation repeated", doc: lintableDocument(), subject: "Oferta!!", code: "subject_excess_punctuation"},
		{name: "subject punctuation many", doc: lintableDocument(), subject: "Oferta! Hoje? Agora!", code: "subject_excess_punctuation"},
		{name: "preheader missing", doc: func() Document { d := lintableDocument(); d.Settings.Preheader = " "; return d }(), subject: "Novidades", code: "preheader_missing"},
		{name: "image alt", doc: func() Document {
			d := lintableDocument()
			d.Blocks = append([]Block{imageBlock("img", "https://example.com/a.png", "")}, d.Blocks...)
			return d
		}(), subject: "Novidades", code: "image_missing_alt", blockID: "img"},
		{name: "image alt in columns", doc: func() Document {
			d := lintableDocument()
			d.Blocks = append([]Block{columnsBlock("c", Ratio5050, []Block{imageBlock("ci", "https://example.com/a.png", " ")}, nil)}, d.Blocks...)
			return d
		}(), subject: "Novidades", code: "image_missing_alt", blockID: "ci"},
		{name: "image heavy", doc: docWith(imageBlock("i1", "https://example.com/a.png", "a"), imageBlock("i2", "https://example.com/b.png", "b"), buttonBlock("b", "Ver", "https://example.com")), subject: "Novidades", code: "image_heavy"},
		{name: "too many links", doc: docWith(textBlock("t", manyLinks...)), subject: "Novidades", code: "too_many_links"},
		{name: "no call to action", doc: docWith(textBlock("t", Run{Text: "Somente texto"})), subject: "Novidades", code: "no_call_to_action"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hints := Lint(tc.doc, tc.subject)
			if !hasHint(hints, tc.code, tc.blockID) {
				t.Fatalf("expected hint %s on %q, got %+v", tc.code, tc.blockID, hints)
			}
		})
	}
}

func TestLintDoesNotFlagCalmSubjects(t *testing.T) {
	for _, subject := range []string{"Oferta para voce!", "Qual o seu plano?", "PIX liberado no app", "Oi {{FIRST_NAME}}"} {
		t.Run(subject, func(t *testing.T) {
			for _, hint := range Lint(lintableDocument(), subject) {
				if strings.HasPrefix(hint.Code, "subject_") {
					t.Fatalf("unexpected hint %+v", hint)
				}
			}
		})
	}
}

func TestLintLinkCountsAsCallToAction(t *testing.T) {
	doc := docWith(textBlock("t", Run{Text: strings.Repeat("Texto ", 40)}, Run{Text: "site", Link: &Link{Href: "https://example.com"}}))
	if hasHint(Lint(doc, "Oi"), "no_call_to_action", "") {
		t.Fatal("a text link is a call to action")
	}
}
