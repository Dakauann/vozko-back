package emaildesign

import (
	"reflect"
	"strings"
	"testing"
)

func audienceDocument() Document {
	return Document{
		Settings: Settings{Preheader: "Oi {{first_name|você}}"},
		Blocks: []Block{
			{ID: "h", Kind: KindHeading, Heading: &Heading{Text: "Olá, {{name}}", Level: 1}},
			{ID: "t", Kind: KindText, Text: &Text{Paragraphs: []Paragraph{{Runs: []Run{{Text: "Série: {{field.serie|a definir}} e {{first_name|cliente}}"}}}}}},
			{ID: "c", Kind: KindColumns, Columns: &Columns{Columns: []Column{{Blocks: []Block{{ID: "b", Kind: KindButton, Button: &Button{Label: "Ver {{company}}", Href: "https://exemplo.com.br/?e={{email}}"}}}}}}},
		},
	}
}

func TestMergeTagsListsEveryTagOnceWithItsFirstFallback(t *testing.T) {
	got := MergeTags("Oferta para {{first_name|cliente}}", audienceDocument())
	names := []string{}
	for _, tag := range got {
		names = append(names, tag.Name)
	}
	if !reflect.DeepEqual(names[:1], []string{"first_name"}) || len(got) != 5 {
		t.Fatalf("tags = %+v, want first_name first (it is in the subject) and five distinct tags", got)
	}
	byName := map[string]MergeTag{}
	for _, tag := range got {
		byName[tag.Name] = tag
	}
	if byName["first_name"].Fallback != "cliente" || byName["field.serie"].Fallback != "a definir" || byName["name"].Fallback != "" || byName["company"].Name != "company" || byName["email"].Name != "email" {
		t.Fatalf("tags = %+v", byName)
	}
	if tags := MergeTags("", Document{}); len(tags) != 0 {
		t.Fatalf("an empty draft has tags %+v", tags)
	}
}

func TestPersonalizationValueIsTheRawValueWithoutFallback(t *testing.T) {
	p := Personalization{FirstName: "Maria", Email: "maria@exemplo.com.br", Fields: map[string]string{"serie": "1º ano"}}
	cases := map[string]string{"first_name": "Maria", "name": "", "email": "maria@exemplo.com.br", "company": "", "field.serie": "1º ano", "field.outra": "", "desconhecida": ""}
	for tag, want := range cases {
		if got := p.Value(tag); got != want {
			t.Fatalf("Value(%q) = %q, want %q", tag, got, want)
		}
	}
}

func TestAFallbackOverrideReplacesTheTagFallbackOnlyWhenTheValueIsEmpty(t *testing.T) {
	p := Personalization{FirstName: "", Name: "Maria Lima", Fallbacks: map[string]string{"first_name": "amigo"}}
	got, err := ExpandText("Oi {{first_name|cliente}}, {{name|você}}", p)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Oi amigo, Maria Lima" {
		t.Fatalf("ExpandText() = %q", got)
	}
	subject, err := RenderSubject("Para {{first_name|cliente}}", Personalization{Fallbacks: map[string]string{"name": "x"}})
	if err != nil || !strings.HasSuffix(subject, "cliente") {
		t.Fatalf("an override of another tag leaves this one alone: %q %v", subject, err)
	}
}
