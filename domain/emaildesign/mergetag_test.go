package emaildesign

import (
	"errors"
	"strings"
	"testing"
)

func TestRenderSubject(t *testing.T) {
	person := Personalization{
		FirstName: "Ana",
		Name:      "Ana Souza",
		Email:     "ana@example.com",
		Company:   "Loja <Bela>",
		Fields:    map[string]string{"plano": "Ouro", "vazio": "  "},
	}
	cases := []struct {
		name    string
		subject string
		person  Personalization
		want    string
	}{
		{name: "plain", subject: "Oferta da semana", person: person, want: "Oferta da semana"},
		{name: "first name", subject: "Oi {{first_name}}", person: person, want: "Oi Ana"},
		{name: "spaces in tag", subject: "Oi {{ name }}", person: person, want: "Oi Ana Souza"},
		{name: "email", subject: "{{email}}", person: person, want: "ana@example.com"},
		{name: "company not escaped in subject", subject: "{{company}}", person: person, want: "Loja <Bela>"},
		{name: "field", subject: "Plano {{field.plano}}", person: person, want: "Plano Ouro"},
		{name: "fallback unused", subject: "Oi {{first_name|cliente}}", person: person, want: "Oi Ana"},
		{name: "fallback used", subject: "Oi {{first_name|cliente}}", person: Personalization{}, want: "Oi cliente"},
		{name: "fallback for blank value", subject: "Oi {{field.vazio|amigo}}", person: person, want: "Oi amigo"},
		{name: "fallback for missing field", subject: "Oi {{field.ausente|amigo}}", person: person, want: "Oi amigo"},
		{name: "empty without fallback", subject: "Oi {{first_name}}!", person: Personalization{}, want: "Oi !"},
		{name: "newline in value", subject: "Oi {{name}}", person: Personalization{Name: "Ana\r\nBcc: x"}, want: "Oi Ana  Bcc: x"},
		{name: "stray closing braces", subject: "a }} b", person: person, want: "a }} b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RenderSubject(tc.subject, tc.person)
			if err != nil {
				t.Fatalf("render subject: %v", err)
			}
			if got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestRenderSubjectRejectsInvalid(t *testing.T) {
	for _, subject := range []string{"", "Oi {{sobrenome}}", "Oi {{first_name", "a\nb", strings.Repeat("a", 151)} {
		t.Run(subject, func(t *testing.T) {
			_, err := RenderSubject(subject, Personalization{})
			if !errors.Is(err, ErrInvalidSubject) {
				t.Fatalf("expected ErrInvalidSubject, got %v", err)
			}
			var verr *ValidationError
			if !errors.As(err, &verr) || len(verr.Issues) == 0 {
				t.Fatalf("expected issues in the error, got %v", err)
			}
		})
	}
}

func TestRenderEscapesSubstitutedValues(t *testing.T) {
	doc := docWith(headingBlock("h", "Oi {{first_name}} {{field.cupom|<sem>}}"))
	in := fullInput()
	in.Personalization = Personalization{FirstName: `<b onmouseover="x">Ana</b>`}
	out, err := Render(doc, in)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(out.HTML, "<b onmouseover") {
		t.Fatal("substituted value was not escaped")
	}
	if !strings.Contains(out.HTML, "&lt;b onmouseover=&#34;x&#34;&gt;Ana&lt;/b&gt;") {
		t.Fatal("escaped value missing from HTML")
	}
	if !strings.Contains(out.HTML, "&lt;sem&gt;") {
		t.Fatal("fallback was not escaped")
	}
	if !strings.Contains(out.Text, `Oi <b onmouseover="x">Ana</b> <sem>`) {
		t.Fatalf("plain text should carry raw values, got %q", out.Text)
	}
}

func TestRenderEscapesValuesInsideLinks(t *testing.T) {
	doc := docWith(buttonBlock("b", "Ir", "https://example.com/?e={{email}}&p={{field.plano|basico}}"))
	in := fullInput()
	in.Personalization = Personalization{Email: "a+b@example.com", Fields: map[string]string{"plano": `x"><script>`}}
	out, err := Render(doc, in)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out.HTML, `href="https://example.com/?e=a%2Bb%40example.com&amp;p=x%22%3E%3Cscript%3E"`) {
		t.Fatalf("link values not query escaped: %s", out.HTML)
	}
	if strings.Contains(out.HTML, "<script") {
		t.Fatal("script tag leaked")
	}
}
