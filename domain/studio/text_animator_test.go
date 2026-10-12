package studio

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"vozko/domain/mediagen"
)

func textAnimatorsDoc(t *testing.T) VideoDocument {
	t.Helper()
	raw, err := os.ReadFile("testdata/editor_text_animators.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc VideoDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func cascadeClip(d *VideoDocument) *Clip { return &d.Tracks[0].Clips[0] }
func cascade(d *VideoDocument) *TextUnitAnimator {
	return &cascadeClip(d).TextAnimators[0]
}
func shaker(d *VideoDocument) *TextUnitAnimator { return &cascadeClip(d).TextAnimators[2] }

func TestTheEditorTextAnimatorDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_text_animators.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindVideo, raw); err != nil {
		t.Fatalf("the editor saved text animators the backend refuses: %v", err)
	}
}

func TestTheEditorTextAnimatorRefusals(t *testing.T) {
	cases := map[string]struct {
		code   string
		change func(*VideoDocument)
	}{
		"animators on a shape layer": {CodeInvalid, func(d *VideoDocument) {
			cascadeClip(d).Layer = &Layer{ID: "l-cascade", Type: LayerShape, Shape: ShapeRect, Fill: "#ffffff", Transform: box()}
			cascadeClip(d).TextIn = nil
		}},
		"an unknown order":           {CodeUnknown, func(d *VideoDocument) { cascade(d).Selector.Order = ptr("spiral") }},
		"a start past 100 percent":   {CodeOutOfRange, func(d *VideoDocument) { cascade(d).Selector.Start = ptr(120.0) }},
		"a range without a start":    {CodeOutOfRange, func(d *VideoDocument) { cascade(d).Selector.Start = nil }},
		"a wiggly with range fields": {CodeInvalid, func(d *VideoDocument) { shaker(d).Selector.Order = ptr("forward") }},
		"a range with a loop":        {CodeInvalid, func(d *VideoDocument) { cascade(d).Selector.LoopMS = ptr(int64(1000)) }},
		"a seed below zero":          {CodeOutOfRange, func(d *VideoDocument) { cascade(d).Selector.Seed = -1 }},
		"a short color":              {CodeInvalid, func(d *VideoDocument) { cascadeClip(d).TextAnimators[1].Properties.Fill = ptr("#fff") }},
		"a stroke on a text without stroke": {CodeInvalid, func(d *VideoDocument) {
			cascadeClip(d).Layer.StrokeWidth = 0
			cascadeClip(d).Layer.Stroke = ""
		}},
		"a scale past the limit":  {CodeOutOfRange, func(d *VideoDocument) { cascade(d).Properties.Scale = ptr(5000.0) }},
		"a repeated id":           {CodeInvalid, func(d *VideoDocument) { shaker(d).ID = "anim-cascade" }},
		"a wiggle faster than 20": {CodeOutOfRange, func(d *VideoDocument) { shaker(d).Selector.Rate = ptr(25.0) }},
		"an offset key on a wiggly": {CodeInvalid, func(d *VideoDocument) {
			shaker(d).Keyframes = map[string][]mediagen.Keyframe{"offset": {{AtMS: 0, Value: 0, Easing: mediagen.EaseLinear}}}
		}},
		"an amount key past the limit": {CodeOutOfRange, func(d *VideoDocument) {
			cascade(d).Keyframes["amount"] = []mediagen.Keyframe{{AtMS: 0, Value: 300, Easing: mediagen.EaseLinear}}
		}},
		"an unknown unit":      {CodeUnknown, func(d *VideoDocument) { cascade(d).Unit = "paragraph" }},
		"no selector":          {CodeInvalid, func(d *VideoDocument) { cascade(d).Selector = nil }},
		"an unknown selector":  {CodeUnknown, func(d *VideoDocument) { cascade(d).Selector.Kind = "spline" }},
		"index past the limit": {CodeOutOfRange, func(d *VideoDocument) { cascadeClip(d).TextAnimators[1].Selector.End = ptr(200_000.0) }},
	}
	for name, tc := range cases {
		d := textAnimatorsDoc(t)
		tc.change(&d)
		if got := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks]; got != tc.code {
			t.Errorf("%s: got %q, want %q", name, got, tc.code)
		}
	}
}

func TestTextAnimatorsRefuseUnknownFields(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_text_animators.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct{ from, to string }{
		{`"tracking": 6`, `"tracking": 6, "skew": 4`},
		{`"loopMs": 2000,`, `"loopMs": 2000.5,`},
	} {
		edited := strings.Replace(string(raw), change.from, change.to, 1)
		if edited == string(raw) {
			t.Fatalf("the fixture no longer has %s", change.from)
		}
		if err := ValidateDocument(KindVideo, []byte(edited)); err == nil {
			t.Fatalf("%s should be refused", change.to)
		}
	}
}

func TestTextAnimatorListsMatchTheEditor(t *testing.T) {
	want := map[string][]string{
		"kinds":  {"range", "wiggly"},
		"orders": {"forward", "backward", "centerOut", "edgesIn", "random"},
		"shapes": {"square", "rampUp", "rampDown", "triangle", "round", "smooth"},
		"units":  {"percent", "index"},
	}
	got := map[string][]string{"kinds": SelectorKinds(), "orders": RangeOrders(), "shapes": RangeShapes(), "units": RangeUnits()}
	for name, list := range want {
		if strings.Join(got[name], ",") != strings.Join(list, ",") {
			t.Fatalf("%s = %v, want %v", name, got[name], list)
		}
	}
}
