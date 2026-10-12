package studio

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"vozko/domain/mediagen"
)

func textMotionDoc(t *testing.T) VideoDocument {
	t.Helper()
	raw, err := os.ReadFile("testdata/editor_text_motion.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc VideoDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func moneyClip(d *VideoDocument) *Clip  { return &d.Tracks[0].Clips[0] }
func arcClip(d *VideoDocument) *Clip    { return &d.Tracks[1].Clips[0] }
func ringClip(d *VideoDocument) *Clip   { return &d.Tracks[1].Clips[1] }
func customClip(d *VideoDocument) *Clip { return &d.Tracks[1].Clips[2] }

func countingText() Clip {
	layer := Layer{ID: "l", Type: LayerText, Text: "0"}
	return Clip{Type: ClipOverlay, Layer: &layer, Counter: &Counter{Locale: "pt-BR", Grouping: true}}
}

func countKeys() []mediagen.Keyframe {
	return []mediagen.Keyframe{{AtMS: 0, Value: 0, Easing: mediagen.EaseOut}, {AtMS: 900, Value: 10_000, Easing: mediagen.EaseLinear}}
}

func TestTheEditorTextMotionDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_text_motion.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindVideo, raw); err != nil {
		t.Fatalf("the editor saved a counter or a text path the backend refuses: %v", err)
	}
}

func TestTheEditorTextMotionDocumentRefusals(t *testing.T) {
	cases := map[string]struct {
		code   string
		change func(*VideoDocument)
	}{
		"a counter on a shape layer": {CodeInvalid, func(d *VideoDocument) {
			moneyClip(d).Layer = &Layer{ID: "l-money", Type: LayerShape, Shape: ShapeRect, Fill: "#ffffff", Transform: box()}
		}},
		"a curved text with a text path":  {CodeInvalid, func(d *VideoDocument) { arcClip(d).Layer.Curve = 0.3 }},
		"counter keys without a counter":  {CodeInvalid, func(d *VideoDocument) { moneyClip(d).Counter = nil }},
		"path offset keys without a path": {CodeInvalid, func(d *VideoDocument) { arcClip(d).TextPath = nil }},
		"an unknown locale":               {CodeUnknown, func(d *VideoDocument) { moneyClip(d).Counter.Locale = "fr-FR" }},
		"four decimals":                   {CodeOutOfRange, func(d *VideoDocument) { moneyClip(d).Counter.Decimals = 4 }},
		"a prefix with a newline":         {CodeInvalid, func(d *VideoDocument) { moneyClip(d).Counter.Prefix = "R$" + string(rune(10)) }},
		"a custom shape without a path":   {CodeInvalid, func(d *VideoDocument) { customClip(d).TextPath.Path = "" }},
		"an arc without a bend":           {CodeInvalid, func(d *VideoDocument) { arcClip(d).TextPath.Bend = nil }},
		"a circle with a bend": {CodeInvalid, func(d *VideoDocument) {
			bend := 0.5
			ringClip(d).TextPath.Bend = &bend
		}},
		"an offset of five": {CodeOutOfRange, func(d *VideoDocument) { ringClip(d).TextPath.Offset = 5 }},
	}
	for name, tc := range cases {
		d := textMotionDoc(t)
		tc.change(&d)
		if got := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks]; got != tc.code {
			t.Errorf("%s: got %q, want %q", name, got, tc.code)
		}
	}
}

func TestCounterRules(t *testing.T) {
	if code := countingText().counterIssue(); code != "" {
		t.Fatalf("a plain counter was refused: %s", code)
	}
	cases := map[string]struct {
		code   string
		change func(*Clip)
	}{
		"keys without a counter": {CodeInvalid, func(c *Clip) {
			c.Counter = nil
			c.Keyframes = &ClipKeyframes{Counter: countKeys()}
		}},
		"a counter on a shape":       {CodeInvalid, func(c *Clip) { c.Layer.Type = LayerShape }},
		"a counter without a layer":  {CodeInvalid, func(c *Clip) { c.Layer = nil }},
		"a counter on an image":      {CodeInvalid, func(c *Clip) { c.Type = ClipImage }},
		"an unknown locale":          {CodeUnknown, func(c *Clip) { c.Counter.Locale = "pt-PT" }},
		"negative decimals":          {CodeOutOfRange, func(c *Clip) { c.Counter.Decimals = -1 }},
		"four decimals":              {CodeOutOfRange, func(c *Clip) { c.Counter.Decimals = MaxCounterDecimals + 1 }},
		"a value past the top":       {CodeOutOfRange, func(c *Clip) { c.Counter.Value = MaxCounterValue * 2 }},
		"a value past the bottom":    {CodeOutOfRange, func(c *Clip) { c.Counter.Value = MinCounterValue * 2 }},
		"a tab in the prefix":        {CodeInvalid, func(c *Clip) { c.Counter.Prefix = string(rune(9)) + "R$" }},
		"a delete in the suffix":     {CodeInvalid, func(c *Clip) { c.Counter.Suffix = "%" + string(rune(0x7f)) }},
		"a C1 control in the suffix": {CodeInvalid, func(c *Clip) { c.Counter.Suffix = string(rune(0x85)) }},
		"a line separator":           {CodeInvalid, func(c *Clip) { c.Counter.Prefix = string(rune(0x2028)) }},
		"a paragraph separator":      {CodeInvalid, func(c *Clip) { c.Counter.Suffix = string(rune(0x2029)) }},
		"a long prefix":              {CodeTooLarge, func(c *Clip) { c.Counter.Prefix = strings.Repeat("é", MaxCounterAffixRunes+1) }},
		"a long suffix":              {CodeTooLarge, func(c *Clip) { c.Counter.Suffix = strings.Repeat("a", MaxCounterAffixRunes+1) }},
	}
	for name, tc := range cases {
		c := countingText()
		tc.change(&c)
		if got := c.counterIssue(); got != tc.code {
			t.Errorf("%s: got %q, want %q", name, got, tc.code)
		}
	}
	edges := countingText()
	edges.Counter.Decimals = MaxCounterDecimals
	edges.Counter.Value = MaxCounterValue
	edges.Counter.Prefix = strings.Repeat("é", MaxCounterAffixRunes)
	edges.Counter.Suffix = " mil"
	if code := edges.counterIssue(); code != "" {
		t.Fatalf("a counter at its limits was refused: %s", code)
	}
}

func TestCounterKeysStayInRange(t *testing.T) {
	d := textMotionDoc(t)
	moneyClip(&d).Keyframes.Counter[1].Value = MaxCounterValue * 2
	if got := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks]; got != CodeOutOfRange {
		t.Fatalf("a counter key past the top: got %q", got)
	}
}

func TestCounterLocalesMatchTheEditor(t *testing.T) {
	want := []string{"pt-BR", "en-US", "es-ES", "de-DE"}
	got := CounterLocales()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("locales drifted: %v", got)
	}
}

func TestCounterFieldsAreStrict(t *testing.T) {
	raw := strings.Replace(string(mustJSON(t, textMotionDoc(t))), `"counter":{`, `"counter":{"currency":"BRL",`, 1)
	if codeOf(t, ValidateDocument(KindVideo, []byte(raw)))[FieldDocument] == "" {
		t.Fatal("an unknown counter field was accepted")
	}
}
