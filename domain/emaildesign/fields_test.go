package emaildesign

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func rawFields(t *testing.T, raw string) map[string]json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func TestBlockFieldsFlattenTheKindAndTheTextMarkup(t *testing.T) {
	block := Block{ID: "intro", Kind: KindText, Padding: PaddingLarge, BackgroundColor: "#ffffff", Text: &Text{Align: AlignCenter, Paragraphs: []Paragraph{
		{Runs: []Run{{Text: "Oi "}, {Text: "você", Bold: true}}},
		{Runs: []Run{{Text: "Veja", Link: &Link{Href: "https://x.com"}}}},
	}}}
	got := block.Fields()
	want := map[string]interface{}{"id": "intro", "kind": "text", "padding": "l", "background_color": "#ffffff", "align": "center", "text": "Oi **você**\n\n[Veja](https://x.com)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}

func TestColumnsFieldsNestTheirChildren(t *testing.T) {
	block := columnsBlock("cols", Ratio3367, []Block{headingBlock("a", "Um")}, []Block{})
	got := block.Fields()
	children, ok := got["columns"].([][]map[string]interface{})
	if !ok || len(children) != 2 || children[0][0]["text"] != "Um" || len(children[1]) != 0 || got["ratio"] != "33-67" {
		t.Fatalf("got %#v", got)
	}
}

func TestPatchBlockOverlaysOnlyTheGivenFields(t *testing.T) {
	block := Block{ID: "cta", Kind: KindButton, Button: &Button{Label: "Comprar", Href: "https://x.com", Align: AlignLeft, Color: "#111111"}}
	got, err := PatchBlock(block, rawFields(t, `{"label":"Ver ofertas","color":"","full_width":true,"padding":"s"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := Block{ID: "cta", Kind: KindButton, Padding: PaddingSmall, Button: &Button{Label: "Ver ofertas", Href: "https://x.com", Align: AlignLeft, FullWidth: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got.Button)
	}
	if block.Button.Label != "Comprar" {
		t.Fatal("the original block changed")
	}
}

func TestPatchBlockParsesTextMarkup(t *testing.T) {
	got, err := PatchBlock(textBlock("t", Run{Text: "velho"}), rawFields(t, `{"text":"**novo**"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Text.Paragraphs, []Paragraph{{Runs: []Run{{Text: "novo", Bold: true}}}}) {
		t.Fatalf("got %+v", got.Text.Paragraphs)
	}
}

func TestPatchBlockRedistributesColumnsOnRatio(t *testing.T) {
	block := columnsBlock("cols", Ratio333333, []Block{headingBlock("a", "A")}, []Block{headingBlock("b", "B")}, []Block{headingBlock("c", "C")})
	got, err := PatchBlock(block, rawFields(t, `{"ratio":"50-50"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Columns.Ratio != Ratio5050 || len(got.Columns.Columns) != 2 || len(got.Columns.Columns[1].Blocks) != 2 || got.Columns.Columns[1].Blocks[1].ID != "c" {
		t.Fatalf("got %+v", got.Columns)
	}
	wider, err := PatchBlock(got, rawFields(t, `{"ratio":"33-33-33"}`))
	if err != nil || len(wider.Columns.Columns) != 3 || len(wider.Columns.Columns[2].Blocks) != 0 {
		t.Fatalf("got %+v, %v", wider.Columns, err)
	}
}

func TestPatchBlockRefusesFieldsOutsideTheKind(t *testing.T) {
	cases := map[string]struct {
		block Block
		raw   string
		code  string
		field string
	}{
		"unknown":          {block: headingBlock("h", "Oi"), raw: `{"label":"x"}`, code: "field_not_for_kind", field: "label"},
		"paragraphs":       {block: textBlock("t", Run{Text: "x"}), raw: `{"paragraphs":[]}`, code: "field_not_for_kind", field: "paragraphs"},
		"column children":  {block: columnsBlock("c", Ratio5050, nil, nil), raw: `{"columns":[]}`, code: "field_not_for_kind", field: "columns"},
		"wrong type":       {block: headingBlock("h", "Oi"), raw: `{"level":"um"}`, code: "field_invalid", field: "level"},
		"bad markup":       {block: textBlock("t", Run{Text: "x"}), raw: `{"text":"**oi"}`, code: "text_markup_invalid", field: "text"},
		"bad ratio":        {block: columnsBlock("c", Ratio5050, nil, nil), raw: `{"ratio":"10-90"}`, code: "field_invalid", field: "ratio"},
		"identity locked":  {block: headingBlock("h", "Oi"), raw: `{"id":"x"}`, code: "field_not_for_kind", field: "id"},
		"padding not text": {block: headingBlock("h", "Oi"), raw: `{"padding":3}`, code: "field_invalid", field: "padding"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := PatchBlock(tc.block, rawFields(t, tc.raw))
			var fieldErr *FieldError
			if !errors.As(err, &fieldErr) || fieldErr.Code != tc.code || fieldErr.Field != tc.field {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestNewBlockStartsFromKindDefaults(t *testing.T) {
	image, err := NewBlock("image-1", KindImage, rawFields(t, `{"src":"https://cdn.example.com/a.jpg","alt":"Tênis"}`))
	if err != nil {
		t.Fatal(err)
	}
	if image.Image.Width != MaxImageWidth || image.Image.Alt != "Tênis" {
		t.Fatalf("got %+v", image.Image)
	}
	columns, err := NewBlock("cols", KindColumns, nil)
	if err != nil || columns.Columns.Ratio != Ratio5050 || len(columns.Columns.Columns) != 2 {
		t.Fatalf("got %+v, %v", columns.Columns, err)
	}
	spacer, err := NewBlock("gap", KindSpacer, nil)
	if err != nil || spacer.Spacer.Height != DefaultSpacerHeight {
		t.Fatalf("got %+v, %v", spacer.Spacer, err)
	}
	if _, err := NewBlock("x", BlockKind("video"), nil); !errors.Is(err, ErrUnknownBlockKind) {
		t.Fatalf("got %v", err)
	}
}

func TestPatchSettingsOverlaysTheGivenFields(t *testing.T) {
	got, err := PatchSettings(Settings{Width: 600, TextColor: "#111111"}, rawFields(t, `{"preheader":"Só hoje","text_color":"","font_family":"georgia"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, Settings{Width: 600, Preheader: "Só hoje", FontFamily: "georgia"}) {
		t.Fatalf("got %+v", got)
	}
	var fieldErr *FieldError
	if _, err := PatchSettings(Settings{}, rawFields(t, `{"blocks":[]}`)); !errors.As(err, &fieldErr) || fieldErr.Code != "field_not_for_kind" {
		t.Fatalf("got %v", err)
	}
}
