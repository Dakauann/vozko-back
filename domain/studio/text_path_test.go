package studio

import (
	"strings"
	"testing"

	"vozko/domain/mediagen"
)

func pathedText() Clip {
	layer := Layer{ID: "l", Type: LayerText, Text: "Oferta"}
	bend := 0.5
	return Clip{Type: ClipOverlay, Layer: &layer, TextPath: &TextPath{Shape: TextPathArc, Bend: &bend, Offset: 0.5, Align: "center"}}
}

func TestTextPathRules(t *testing.T) {
	if code := pathedText().textPathIssue(); code != "" {
		t.Fatalf("a plain text path was refused: %s", code)
	}
	cases := map[string]struct {
		code   string
		change func(*Clip)
	}{
		"keys without a path": {CodeInvalid, func(c *Clip) {
			c.TextPath = nil
			c.Keyframes = &ClipKeyframes{PathOffset: []mediagen.Keyframe{{AtMS: 0, Value: 0, Easing: mediagen.EaseLinear}}}
		}},
		"a path on a shape":       {CodeInvalid, func(c *Clip) { c.Layer.Type = LayerShape }},
		"a path without a layer":  {CodeInvalid, func(c *Clip) { c.Layer = nil }},
		"a path on a video":       {CodeInvalid, func(c *Clip) { c.Type = ClipVideo }},
		"a path on a curved text": {CodeInvalid, func(c *Clip) { c.Layer.Curve = -0.2 }},
		"an unknown shape":        {CodeUnknown, func(c *Clip) { c.TextPath.Shape = "spiral" }},
		"an unknown align":        {CodeUnknown, func(c *Clip) { c.TextPath.Align = "justify" }},
		"an offset past four":     {CodeOutOfRange, func(c *Clip) { c.TextPath.Offset = MaxTextPathOffset + 0.5 }},
		"an offset below four":    {CodeOutOfRange, func(c *Clip) { c.TextPath.Offset = MinTextPathOffset - 0.5 }},
		"an arc without a bend":   {CodeInvalid, func(c *Clip) { c.TextPath.Bend = nil }},
		"a bend past one": {CodeOutOfRange, func(c *Clip) {
			bend := 1.5
			c.TextPath.Bend = &bend
		}},
		"a circle with a bend": {CodeInvalid, func(c *Clip) { c.TextPath.Shape = TextPathCircle }},
		"a wave without a bend": {CodeInvalid, func(c *Clip) {
			c.TextPath.Shape = TextPathWave
			c.TextPath.Bend = nil
		}},
		"an arc with a path": {CodeInvalid, func(c *Clip) { c.TextPath.Path = "M0 0 L1 1" }},
		"a custom without a path": {CodeInvalid, func(c *Clip) {
			c.TextPath.Shape = TextPathCustom
			c.TextPath.Bend = nil
		}},
		"a custom with a broken path": {CodeInvalid, func(c *Clip) {
			c.TextPath.Shape = TextPathCustom
			c.TextPath.Bend = nil
			c.TextPath.Path = "L0 0 Q1"
		}},
	}
	for name, tc := range cases {
		c := pathedText()
		tc.change(&c)
		if got := c.textPathIssue(); got != tc.code {
			t.Errorf("%s: got %q, want %q", name, got, tc.code)
		}
	}
	accepted := map[string]func(*Clip){
		"a circle": func(c *Clip) {
			c.TextPath.Shape = TextPathCircle
			c.TextPath.Bend = nil
		},
		"a wave at the lowest bend": func(c *Clip) {
			bend := MinTextPathBend
			c.TextPath.Shape = TextPathWave
			c.TextPath.Bend = &bend
		},
		"a custom curve at the far offset": func(c *Clip) {
			c.TextPath.Shape = TextPathCustom
			c.TextPath.Bend = nil
			c.TextPath.Path = "M0 0.8 C0.3 0.1 0.7 0.1 1 0.8"
			c.TextPath.Offset = MinTextPathOffset
			c.TextPath.Align = "end"
		},
	}
	for name, change := range accepted {
		c := pathedText()
		change(&c)
		if got := c.textPathIssue(); got != "" {
			t.Errorf("%s refused: %s", name, got)
		}
	}
}

func TestPathOffsetKeysStayInRange(t *testing.T) {
	d := textMotionDoc(t)
	arcClip(&d).Keyframes.PathOffset[1].Value = MaxTextPathOffset + 1
	if got := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks]; got != CodeOutOfRange {
		t.Fatalf("a path offset key past four: got %q", got)
	}
}

func TestTextMotionStaysOffAudioClips(t *testing.T) {
	cases := map[string]func(*Clip){
		"a counter on a song":    func(c *Clip) { c.Counter = &Counter{Locale: "pt-BR"} },
		"a text path on a song":  func(c *Clip) { c.TextPath = &TextPath{Shape: TextPathCircle, Align: "center"} },
		"counter keys on a song": func(c *Clip) { c.Keyframes = &ClipKeyframes{Counter: countKeys()} },
		"offset keys on a song": func(c *Clip) {
			c.Keyframes = &ClipKeyframes{PathOffset: []mediagen.Keyframe{{AtMS: 0, Value: 0, Easing: mediagen.EaseLinear}}}
		},
	}
	for name, change := range cases {
		d := videoDoc()
		change(&d.Tracks[2].Clips[0])
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks] != CodeInvalid {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestTextPathListsMatchTheEditor(t *testing.T) {
	if got := strings.Join(TextPathShapes(), ","); got != "arc,circle,wave,custom" {
		t.Fatalf("shapes drifted: %s", got)
	}
	if got := strings.Join(TextPathAligns(), ","); got != "start,center,end" {
		t.Fatalf("aligns drifted: %s", got)
	}
}

func TestTextPathFieldsAreStrict(t *testing.T) {
	raw := strings.Replace(string(mustJSON(t, textMotionDoc(t))), `"textPath":{`, `"textPath":{"spacing":2,`, 1)
	if codeOf(t, ValidateDocument(KindVideo, []byte(raw)))[FieldDocument] == "" {
		t.Fatal("an unknown text path field was accepted")
	}
}
