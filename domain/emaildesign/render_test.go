package emaildesign

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

var hrefPattern = regexp.MustCompile(`href="([^"]*)"`)

func renderFull(t *testing.T, in RenderInput) Rendered {
	t.Helper()
	out, err := Render(fullDocument(), in)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return out
}

func TestRenderProducesEmailSafeHTML(t *testing.T) {
	out := renderFull(t, fullInput())
	html := out.HTML
	mustContain := []string{
		`<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN"`,
		`lang="pt-BR"`,
		`<meta http-equiv="Content-Type" content="text/html; charset=UTF-8" />`,
		`<meta name="viewport" content="width=device-width, initial-scale=1.0" />`,
		`<meta name="color-scheme" content="light" />`,
		`<meta name="supported-color-schemes" content="light" />`,
		`<title>Oferta para Ana</title>`,
		`@media only screen and (max-width: 600px)`,
		`Novidades de Ana`,
		`&zwnj;`,
		`mso-hide:all`,
		`max-width:600px`,
		`<!--[if mso]>`,
		`<v:roundrect`,
		`fillcolor="#2563eb"`,
		`fillcolor="#16a34a"`,
		`href="https://mail.example.com/u/abc123"`,
		`>Descadastrar</a>`,
		`Acme &amp; Filhos`,
		`Rua &lt;A&gt;, 1`,
		`Voce recebe porque assinou, Ana Souza.`,
		`alt="Produto"`,
		`class="column"`,
		`<td width="270" valign="top"`,
		`<td width="180" valign="top"`,
		`<strong>a bordo</strong>`,
		`<em> e </em>`,
		`text-decoration:underline;">veja</span>`,
		`href="mailto:contato@example.com"`,
		`href="tel:+5511999999999"`,
		`Inter, Arial, Helvetica, sans-serif`,
		`bgcolor="#fafafa"`,
		`color:#222222`,
		`Instagram</a>`,
		`Nosso site</a>`,
		`width="432"`,
	}
	for _, want := range mustContain {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	for _, banned := range []string{"<script", "javascript:", "<iframe", "<form", "onerror", "onclick", "\u2014", "\u2013"} {
		if strings.Contains(strings.ToLower(html), strings.ToLower(banned)) {
			t.Errorf("HTML contains banned %q", banned)
		}
	}
	if strings.Count(html, "<table") != strings.Count(html, `<table role="presentation"`) {
		t.Error("every table must be role presentation")
	}
	if strings.Count(html, "<img") != strings.Count(html, "<img src=") || strings.Count(html, "<img") != strings.Count(html, " alt=") {
		t.Error("every image must carry src and alt")
	}
	if strings.Count(html, "<!--[if mso]>") == 0 || strings.Count(html, "<![endif]-->") < strings.Count(html, "<!--[if mso]>") {
		t.Error("conditional comments unbalanced")
	}
}

func TestRenderLinksUseAllowedSchemes(t *testing.T) {
	out := renderFull(t, fullInput())
	matches := hrefPattern.FindAllStringSubmatch(out.HTML, -1)
	if len(matches) < 8 {
		t.Fatalf("expected the links to be rendered, got %d", len(matches))
	}
	for _, m := range matches {
		raw := strings.ReplaceAll(m[1], "&amp;", "&")
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("unparsable href %q: %v", raw, err)
		}
		switch u.Scheme {
		case "https", "mailto", "tel":
		default:
			t.Errorf("href %q uses scheme %q", raw, u.Scheme)
		}
	}
}

func TestRenderAppliesUTM(t *testing.T) {
	in := fullInput()
	in.UTM = &UTM{Source: "email", Medium: "newsletter", Campaign: "black friday"}
	in.ViewInBrowserURL = "https://mail.example.com/v/abc"
	in.ViewInBrowserLabel = "Ver no navegador"
	out := renderFull(t, in)
	wants := []string{
		`href="https://example.com/loja?ref=1&amp;utm_source=email&amp;utm_medium=newsletter&amp;utm_campaign=black+friday"`,
		`href="https://example.com/site?utm_source=email&amp;utm_medium=newsletter&amp;utm_campaign=black+friday"`,
		`href="https://mail.example.com/u/abc123"`,
		`href="https://mail.example.com/v/abc"`,
		`href="mailto:contato@example.com"`,
		`href="tel:+5511999999999"`,
	}
	for _, want := range wants {
		if !strings.Contains(out.HTML, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	if strings.Contains(out.HTML, "abc123?utm") || strings.Contains(out.HTML, "abc?utm") {
		t.Error("UTM leaked onto unsubscribe or view in browser link")
	}
	if !strings.Contains(out.Text, "Comprar agora (https://example.com/loja?ref=1&utm_source=email&utm_medium=newsletter&utm_campaign=black+friday)") {
		t.Errorf("text alternative missing UTM link: %s", out.Text)
	}
}

func TestRenderTextAlternative(t *testing.T) {
	in := fullInput()
	in.ViewInBrowserURL = "https://mail.example.com/v/abc"
	in.ViewInBrowserLabel = "Ver no navegador"
	out := renderFull(t, in)
	wants := []string{
		"Ver no navegador (https://mail.example.com/v/abc)",
		"Oi Ana",
		"Bem-vindo a bordo e veja",
		"nosso site (https://example.com/site) ou escreva (mailto:contato@example.com)",
		"Comprar agora (https://example.com/loja?ref=1)",
		"Ligar (tel:+5511999999999)",
		"Produto (https://example.com/produto)",
		"Esquerda",
		"Ver (https://example.com/ver)",
		"Instagram (https://instagram.com/acme)",
		"Nosso site (https://acme.example.com)",
		"Acme & Filhos",
		"Rua <A>, 1",
		"Descadastrar (https://mail.example.com/u/abc123)",
	}
	for _, want := range wants {
		if !strings.Contains(out.Text, want) {
			t.Errorf("text missing %q\n%s", want, out.Text)
		}
	}
	if strings.Contains(out.Text, "<td") || strings.Contains(out.Text, "<p") || strings.Contains(out.Text, "&amp;") {
		t.Error("text alternative contains markup")
	}
	if strings.Contains(out.Text, "\n\n\n") {
		t.Error("text alternative has runs of blank lines")
	}
	if strings.Index(out.Text, "Oi Ana") > strings.Index(out.Text, "Descadastrar") {
		t.Error("text alternative out of order")
	}
}

func TestRenderEscapesUserText(t *testing.T) {
	doc := docWith(
		headingBlock("h", `<img src=x onerror=alert(1)>`),
		textBlock("t", Run{Text: "<script>alert(1)</script>\nlinha"}),
		Block{ID: "i", Kind: KindImage, Image: &Image{Src: `https://example.com/a.png?x="><script>`, Alt: `"quoted" & <tag>`, Width: 50}},
	)
	out, err := Render(doc, fullInput())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(out.HTML, "<script") || strings.Contains(out.HTML, "<img src=x") {
		t.Fatal("user text not escaped")
	}
	for _, want := range []string{"&lt;script&gt;alert(1)&lt;/script&gt;<br />linha", `alt="&#34;quoted&#34; &amp; &lt;tag&gt;"`, `src="https://example.com/a.png?x=&#34;&gt;&lt;script&gt;"`} {
		if !strings.Contains(out.HTML, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	in := fullInput()
	in.UTM = &UTM{Source: "s", Medium: "m"}
	first := renderFull(t, in)
	for i := 0; i < 20; i++ {
		again := renderFull(t, in)
		if again != first {
			t.Fatal("render output changed between runs")
		}
	}
}

func TestRenderHonoursSettings(t *testing.T) {
	doc := fullDocument()
	doc.Settings.Width = 640
	doc.Settings.FontFamily = "georgia"
	in := fullInput()
	in.Lang = "en"
	out, err := Render(doc, in)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{`lang="en"`, "max-width:640px", `width="640"`, "Georgia, 'Times New Roman', Times, serif"} {
		if !strings.Contains(out.HTML, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
}

func TestRenderUsesDefaultsForEmptySettings(t *testing.T) {
	in := fullInput()
	in.Subject = ""
	out, err := Render(docWith(headingBlock("h", "Oi")), in)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{"max-width:600px", `lang="pt-BR"`, "Arial, Helvetica, sans-serif", "<title></title>"} {
		if !strings.Contains(out.HTML, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
}

func TestRenderRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name   string
		doc    Document
		mutate func(*RenderInput)
		want   error
	}{
		{name: "invalid document", doc: Document{Version: CurrentVersion}, mutate: func(*RenderInput) {}, want: ErrInvalidDocument},
		{name: "unsubscribe missing", doc: fullDocument(), mutate: func(in *RenderInput) { in.UnsubscribeURL = "" }, want: ErrUnsubscribeURLInvalid},
		{name: "unsubscribe http", doc: fullDocument(), mutate: func(in *RenderInput) { in.UnsubscribeURL = "http://mail.example.com/u" }, want: ErrUnsubscribeURLInvalid},
		{name: "unsubscribe no host", doc: fullDocument(), mutate: func(in *RenderInput) { in.UnsubscribeURL = "https:///u" }, want: ErrUnsubscribeURLInvalid},
		{name: "unsubscribe label", doc: fullDocument(), mutate: func(in *RenderInput) { in.UnsubscribeLabel = " " }, want: ErrUnsubscribeLabelMissing},
		{name: "view url http", doc: fullDocument(), mutate: func(in *RenderInput) { in.ViewInBrowserURL = "http://x.com"; in.ViewInBrowserLabel = "Ver" }, want: ErrViewInBrowserURLInvalid},
		{name: "view label", doc: fullDocument(), mutate: func(in *RenderInput) { in.ViewInBrowserURL = "https://x.com/v" }, want: ErrViewInBrowserLabelMissing},
		{name: "lang", doc: fullDocument(), mutate: func(in *RenderInput) { in.Lang = `pt"><x` }, want: ErrLangInvalid},
		{name: "subject", doc: fullDocument(), mutate: func(in *RenderInput) { in.Subject = "{{sobrenome}}" }, want: ErrInvalidSubject},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := fullInput()
			tc.mutate(&in)
			_, err := Render(tc.doc, in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestRenderInvalidDocumentCarriesIssues(t *testing.T) {
	_, err := Render(Document{Version: CurrentVersion}, fullInput())
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	if !hasIssue(verr.Issues, "footer_missing", "") {
		t.Fatalf("expected footer_missing, got %+v", verr.Issues)
	}
}

func TestRenderAnnotatesBlocksOnlyWhenAsked(t *testing.T) {
	plain := renderFull(t, fullInput())
	if strings.Contains(plain.HTML, "data-block-id") {
		t.Fatal("a sent email must not carry block ids")
	}
	in := fullInput()
	in.Annotate = true
	annotated := renderFull(t, in)
	for _, id := range []string{"h1", "t1", "s1", "c1", "c1h", "c1b", "footer"} {
		if !strings.Contains(annotated.HTML, `data-block-id="`+id+`"`) {
			t.Fatalf("missing the %s block id", id)
		}
	}
}

func TestImageWidthsFollowTheRenderedLayout(t *testing.T) {
	doc := docWith(
		imageBlock("full", "https://cdn.example.com/a.jpg", "A"),
		columnsBlock("cols", Ratio5050, []Block{imageBlock("half", "https://cdn.example.com/b.jpg", "B")}, nil),
	)
	doc.Blocks[0].Image.Width = 50
	widths := ImageWidths(doc)
	if widths["full"] != 270 || widths["half"] != 230 {
		t.Fatalf("got %v", widths)
	}
}

func TestADraftRendersWithItsGapsAndASendDoesNot(t *testing.T) {
	doc := docWith(
		buttonBlock("cta", "Comprar", ""),
		imageBlock("hero", "", ""),
		textBlock("tag", Run{Text: "Oi {{apelido}}"}),
	)
	in := fullInput()
	if _, err := Render(doc, in); !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("a send refuses a document with gaps, got %v", err)
	}
	in.Draft = true
	out, err := Render(doc, in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.HTML, ">Comprar<") || !strings.Contains(out.HTML, "Oi {{apelido}}") {
		t.Fatal("the draft shows its gaps as they are")
	}
	broken := docWith(imageBlock("hero", "http://x.com/a.jpg", "A"))
	if _, err := Render(broken, in); !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("a draft still refuses a broken document, got %v", err)
	}
}
