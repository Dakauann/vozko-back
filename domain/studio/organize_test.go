package studio

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func organizeFixture(t *testing.T) VideoDocument {
	t.Helper()
	raw, err := os.ReadFile("testdata/editor_organize.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc VideoDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestTheEditorOrganizedDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_organize.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindVideo, raw); err != nil {
		t.Fatalf("the editor saved an organized document the backend refuses: %v", err)
	}
	doc := organizeFixture(t)
	shot, song := doc.Tracks[0].Clips[0], doc.Tracks[3].Clips[0]
	if shot.Name != "Abertura na loja" || shot.LabelColor != "orange" || len(shot.Markers) != 2 || shot.Markers[1].DurationMS != 1200 || shot.Markers[0].Color != "yellow" {
		t.Fatalf("clip organization lost: %+v", shot)
	}
	if !song.Markers[0].Beat || song.Markers[2].Note != "entra o coro" {
		t.Fatalf("beat markers lost: %+v", song.Markers)
	}
	if !doc.Tracks[1].Shy || doc.Tracks[1].LabelColor != "cyan" || !doc.Tracks[1].Clips[0].Guide {
		t.Fatalf("track organization lost: %+v", doc.Tracks[1])
	}
	if doc.Markers[0].DurationMS != 2000 || doc.Markers[0].Color != "blue" || !strings.Contains(doc.Markers[1].Note, "preço") {
		t.Fatalf("film markers lost: %+v", doc.Markers)
	}
	again, _ := json.Marshal(doc)
	if err := ValidateDocument(KindVideo, again); err != nil {
		t.Fatalf("a round trip broke the document: %v", err)
	}
}

func TestOrganizationIsStrict(t *testing.T) {
	cases := []struct {
		name  string
		field string
		code  string
		edit  func(*VideoDocument)
	}{
		{"an unknown track label", FieldTracks, CodeUnknown, func(d *VideoDocument) { d.Tracks[0].LabelColor = "teal" }},
		{"an unknown clip label", FieldTracks, CodeUnknown, func(d *VideoDocument) { d.Tracks[0].Clips[0].LabelColor = "teal" }},
		{"a blank clip name", FieldTracks, CodeInvalid, func(d *VideoDocument) { d.Tracks[0].Clips[0].Name = "   " }},
		{"a long clip name", FieldTracks, CodeTooLarge, func(d *VideoDocument) { d.Tracks[2].Clips[0].Name = strings.Repeat("a", 129) }},
		{"a guide null", FieldTracks, CodeInvalid, func(d *VideoDocument) { d.Tracks[2].Clips[0].Guide = true }},
		{"two clip markers with one id", FieldTracks, CodeDuplicate, func(d *VideoDocument) { d.Tracks[3].Clips[0].Markers[1].ID = "b1" }},
		{"an unknown clip marker color", FieldTracks, CodeUnknown, func(d *VideoDocument) { d.Tracks[3].Clips[0].Markers[0].Color = "teal" }},
		{"a negative clip marker", FieldTracks, CodeOutOfRange, func(d *VideoDocument) { d.Tracks[3].Clips[0].Markers[0].AtMS = -1 }},
		{"a negative marker range", FieldMarkers, CodeOutOfRange, func(d *VideoDocument) { d.Markers[0].DurationMS = -5 }},
		{"a marker range past the end", FieldMarkers, CodeOutOfRange, func(d *VideoDocument) { d.Markers[1].DurationMS = MaxVideoMS }},
		{"a long marker note", FieldMarkers, CodeTooLarge, func(d *VideoDocument) { d.Markers[1].Note = strings.Repeat("a", MaxMarkerNoteRunes+1) }},
		{"an unknown marker color", FieldMarkers, CodeUnknown, func(d *VideoDocument) { d.Markers[0].Color = "teal" }},
		{"an inner clip marker out of range", FieldTracks, CodeOutOfRange, func(d *VideoDocument) {
			d.Tracks[0].Clips[1].Tracks[0].Clips[0].Markers[0].DurationMS = MaxClipMarkerMS
		}},
	}
	for _, c := range cases {
		doc := organizeFixture(t)
		c.edit(&doc)
		raw, _ := json.Marshal(doc)
		if got := codeOf(t, ValidateDocument(KindVideo, raw))[c.field]; got != c.code {
			t.Errorf("%s: got %q, want %q", c.name, got, c.code)
		}
	}
}

func TestLabelColorsMatchTheEditor(t *testing.T) {
	if got := strings.Join(LabelColors(), ","); got != "red,orange,yellow,green,cyan,blue,purple,pink" {
		t.Fatalf("label colors drifted: %s", got)
	}
}
