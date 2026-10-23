package emaildesign

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestDecodeAcceptsValidDocument(t *testing.T) {
	raw := []byte(`{
		"version": 1,
		"settings": {"width": 600, "font_family": "georgia", "preheader": "Oi"},
		"blocks": [
			{"id": "h", "kind": "heading", "padding": "s", "background_color": "#fff", "heading": {"text": "Oi", "level": 2, "align": "center"}},
			{"id": "t", "kind": "text", "text": {"paragraphs": [{"runs": [{"text": "a", "bold": true, "link": {"href": "https://example.com"}}]}]}},
			{"id": "c", "kind": "columns", "columns": {"ratio": "50-50", "columns": [{"blocks": [{"id": "s1", "kind": "spacer", "spacer": {"height": 8}}]}, {"blocks": []}]}},
			{"id": "f", "kind": "footer", "footer": {"company_name": "Acme", "address": "Rua A"}}
		]
	}`)
	doc, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Version != 1 || doc.Settings.FontFamily != "georgia" || len(doc.Blocks) != 4 {
		t.Fatalf("unexpected document %+v", doc)
	}
	if doc.Blocks[0].Heading == nil || doc.Blocks[0].Heading.Level != 2 || doc.Blocks[0].Padding != PaddingSmall {
		t.Fatalf("heading not decoded: %+v", doc.Blocks[0])
	}
	if doc.Blocks[1].Text.Paragraphs[0].Runs[0].Link.Href != "https://example.com" {
		t.Fatalf("run link not decoded: %+v", doc.Blocks[1].Text)
	}
	if doc.Blocks[2].Columns.Columns[0].Blocks[0].Spacer.Height != 8 {
		t.Fatalf("nested block not decoded: %+v", doc.Blocks[2].Columns)
	}
	if issues := Validate(doc); len(issues) != 0 {
		t.Fatalf("expected valid document, got %+v", issues)
	}
}

func TestDecodeRejects(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		unknown bool
	}{
		{name: "not json", raw: `{`},
		{name: "empty", raw: ``},
		{name: "array", raw: `[]`},
		{name: "unknown top field", raw: `{"version":1,"blocks":[],"theme":"x"}`},
		{name: "unknown settings field", raw: `{"version":1,"settings":{"accent":"#fff"},"blocks":[]}`},
		{name: "unknown block field", raw: `{"version":1,"blocks":[{"id":"a","kind":"spacer","spacer":{"height":8},"html":"<b>"}]}`},
		{name: "unknown props field", raw: `{"version":1,"blocks":[{"id":"a","kind":"heading","heading":{"text":"a","level":1,"html":"<b>"}}]}`},
		{name: "unknown run field", raw: `{"version":1,"blocks":[{"id":"a","kind":"text","text":{"paragraphs":[{"runs":[{"text":"a","html":"<b>"}]}]}}]}`},
		{name: "unknown kind", raw: `{"version":1,"blocks":[{"id":"a","kind":"html"}]}`, unknown: true},
		{name: "empty kind", raw: `{"version":1,"blocks":[{"id":"a","kind":""}]}`, unknown: true},
		{name: "nested unknown kind", raw: `{"version":1,"blocks":[{"id":"c","kind":"columns","columns":{"ratio":"50-50","columns":[{"blocks":[{"id":"x","kind":"video"}]},{"blocks":[]}]}}]}`, unknown: true},
		{name: "wrong type", raw: `{"version":"1","blocks":[]}`},
		{name: "trailing data", raw: `{"version":1,"blocks":[]} {"version":1}`},
		{name: "trailing garbage", raw: `{"version":1,"blocks":[]}x`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode([]byte(tc.raw))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !errors.Is(err, ErrMalformedDocument) {
				t.Fatalf("expected ErrMalformedDocument, got %v", err)
			}
			if tc.unknown && !errors.Is(err, ErrUnknownBlockKind) {
				t.Fatalf("expected ErrUnknownBlockKind, got %v", err)
			}
		})
	}
}

func TestDecodeRoundTripsStarters(t *testing.T) {
	for _, starter := range StarterDocuments() {
		t.Run(starter.Key, func(t *testing.T) {
			raw, err := json.Marshal(starter.Document)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			back, err := Decode(raw)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !reflect.DeepEqual(back, starter.Document) {
				t.Fatalf("round trip changed the document\nwant %+v\ngot  %+v", starter.Document, back)
			}
		})
	}
}
