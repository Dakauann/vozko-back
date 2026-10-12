package studio

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func textRunsDoc(t *testing.T) VideoDocument {
	t.Helper()
	raw, err := os.ReadFile("testdata/editor_text_runs.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc VideoDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func textRunsImageDoc(t *testing.T) ImageDocument {
	t.Helper()
	raw, err := os.ReadFile("testdata/editor_text_runs_image.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc ImageDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func headlineRuns(d *VideoDocument) []TextRun { return d.Tracks[0].Clips[0].Layer.Runs }
func moneyRunsClip(d *VideoDocument) *Clip    { return &d.Tracks[1].Clips[0] }

func runFlag(v bool) *bool { return &v }

func TestTheEditorTextRunsDocumentsPassTheContract(t *testing.T) {
	video, err := os.ReadFile("testdata/editor_text_runs.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindVideo, video); err != nil {
		t.Fatalf("the editor saved video text runs the backend refuses: %v", err)
	}
	image, err := os.ReadFile("testdata/editor_text_runs_image.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindImage, image); err != nil {
		t.Fatalf("the editor saved image text runs the backend refuses: %v", err)
	}
}

func TestTheEditorTextRunsDocumentRefusals(t *testing.T) {
	cases := map[string]struct {
		code   string
		change func(*VideoDocument)
	}{
		"overlapping runs":                {CodeInvalid, func(d *VideoDocument) { headlineRuns(d)[1].Start = 5 }},
		"unsorted runs":                   {CodeInvalid, func(d *VideoDocument) { r := headlineRuns(d); r[0], r[1] = r[1], r[0] }},
		"a run past the text":             {CodeInvalid, func(d *VideoDocument) { headlineRuns(d)[3].End = 25 }},
		"an empty run":                    {CodeInvalid, func(d *VideoDocument) { headlineRuns(d)[3].End = headlineRuns(d)[3].Start }},
		"a run with no style":             {CodeInvalid, func(d *VideoDocument) { headlineRuns(d)[0] = TextRun{Start: 0, End: 6} }},
		"only underline false":            {CodeInvalid, func(d *VideoDocument) { headlineRuns(d)[0] = TextRun{Start: 0, End: 6, Underline: false} }},
		"a bad color":                     {CodeInvalid, func(d *VideoDocument) { headlineRuns(d)[0].Fill = "yellow" }},
		"an unknown font":                 {CodeUnknown, func(d *VideoDocument) { headlineRuns(d)[2].FontID = "comic" }},
		"a weight of 450":                 {CodeOutOfRange, func(d *VideoDocument) { headlineRuns(d)[0].FontWeight = 450 }},
		"a size of 0.9":                   {CodeOutOfRange, func(d *VideoDocument) { headlineRuns(d)[2].FontSize = 0.9 }},
		"a letter spacing of 3":           {CodeOutOfRange, func(d *VideoDocument) { spacing := 3.0; headlineRuns(d)[2].LetterSpacing = &spacing }},
		"a counter run past the template": {CodeInvalid, func(d *VideoDocument) { moneyRunsClip(d).Layer.Runs[2].End = 9 }},
		"runs on a shape layer": {CodeInvalid, func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Layer = &Layer{ID: "l-shape", Type: LayerShape, Shape: ShapeRect, Fill: "#ffffff", Transform: box(), Runs: []TextRun{{Start: 0, End: 1, Fill: "#000000"}}}
		}},
		"a run past the layer text once the counter is gone": {CodeInvalid, func(d *VideoDocument) {
			clip := moneyRunsClip(d)
			clip.Counter = nil
			clip.Keyframes = nil
			clip.Layer.Runs = []TextRun{{Start: 0, End: 2, Fill: "#ffcc00"}}
		}},
	}
	for name, tc := range cases {
		d := textRunsDoc(t)
		tc.change(&d)
		if got := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks]; got != tc.code {
			t.Errorf("%s: got %q, want %q", name, got, tc.code)
		}
	}
}

func TestTextRunsIndexTheirTemplate(t *testing.T) {
	cases := map[string]func(*VideoDocument){
		"a counter run ending at the template end": func(d *VideoDocument) { moneyRunsClip(d).Layer.Runs[2].End = 8 },
		"a run inside the layer text once the counter is gone": func(d *VideoDocument) {
			clip := moneyRunsClip(d)
			clip.Counter = nil
			clip.Keyframes = nil
			clip.Layer.Runs = []TextRun{{Start: 0, End: 1, Fill: "#ffcc00"}}
		},
		"a run counting an emoji as one character": func(d *VideoDocument) {
			layer := d.Tracks[0].Clips[0].Layer
			layer.Text = strings.Repeat("😀", 3)
			layer.Runs = []TextRun{{Start: 1, End: 3, Caps: true}}
		},
	}
	for name, change := range cases {
		d := textRunsDoc(t)
		change(&d)
		if err := ValidateDocument(KindVideo, mustJSON(t, d)); err != nil {
			t.Errorf("%s: refused: %v", name, err)
		}
	}
}

func TestTheEditorTextRunsImageRefusal(t *testing.T) {
	d := textRunsImageDoc(t)
	d.Artboards[0].Layers[0].Runs[1].Start = 10
	if got := codeOf(t, ValidateDocument(KindImage, mustJSON(t, d)))[FieldLayers]; got != CodeInvalid {
		t.Fatalf("overlapping image runs: got %q", got)
	}
}

func TestTextRunPointerFieldsSurviveTheRoundTrip(t *testing.T) {
	spacing := 0.0
	raw, err := json.Marshal(TextRun{Start: 1, End: 2, Italic: runFlag(false), LetterSpacing: &spacing, Underline: false})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"start":1,"end":2,"italic":false,"letterSpacing":0}`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	var back TextRun
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Italic == nil || *back.Italic || back.LetterSpacing == nil || *back.LetterSpacing != 0 || back.Underline {
		t.Fatalf("the run lost its explicit values: %+v", back)
	}
}
