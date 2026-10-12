package studio

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/mediagen"
)

type presetFixture struct {
	Valid []struct {
		Name      string          `json:"name"`
		Animation json.RawMessage `json:"animation"`
	} `json:"valid"`
	Invalid []struct {
		Reason    string          `json:"reason"`
		Code      string          `json:"code"`
		Animation json.RawMessage `json:"animation"`
	} `json:"invalid"`
}

func loadPresetFixture(t *testing.T) presetFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/editor_animation_presets.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture presetFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func presetCode(t *testing.T, err error) string {
	t.Helper()
	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("not a validation error: %v", err)
	}
	return invalid.Codes()[FieldAnimation]
}

func TestTheSharedPresetFixtureAgreesWithTheValidator(t *testing.T) {
	fixture := loadPresetFixture(t)
	if len(fixture.Valid) < 4 || len(fixture.Invalid) < 8 {
		t.Fatalf("fixture lost cases: %d valid %d invalid", len(fixture.Valid), len(fixture.Invalid))
	}
	for _, c := range fixture.Valid {
		if err := ValidatePresetAnimation(c.Animation); err != nil {
			t.Errorf("%s: %v", c.Name, err)
		}
	}
	for _, c := range fixture.Invalid {
		err := ValidatePresetAnimation(c.Animation)
		if err == nil {
			t.Errorf("%s: accepted", c.Reason)
			continue
		}
		if got := presetCode(t, err); got != c.Code {
			t.Errorf("%s: code %q, want %q", c.Reason, got, c.Code)
		}
	}
}

func TestAnAnimationOutsideTheByteBoundsIsRefused(t *testing.T) {
	if got := presetCode(t, ValidatePresetAnimation(nil)); got != CodeRequired {
		t.Fatalf("empty = %s", got)
	}
	huge := []byte(`{"scope":"visual","durationMs":1000,"motionBlur":true,"pad":"` + strings.Repeat("a", MaxPresetAnimationBytes) + `"}`)
	if got := presetCode(t, ValidatePresetAnimation(huge)); got != CodeTooLarge {
		t.Fatalf("huge = %s", got)
	}
	if got := presetCode(t, ValidatePresetAnimation([]byte(`{"scope":"visual","durationMs":1000,"motionBlur":true}{}`))); got != CodeInvalid {
		t.Fatalf("trailing = %s", got)
	}
	if got := presetCode(t, ValidatePresetAnimation([]byte(`{"scope":"visual","durationMs":600001,"motionBlur":true}`))); got != CodeOutOfRange {
		t.Fatalf("too long = %s", got)
	}
}

func TestTheProbeKeepsTheCodeOfTheClipRule(t *testing.T) {
	cases := map[string]struct {
		raw  string
		code string
	}{
		"motion longer than the preset": {`{"scope":"visual","durationMs":500,"motionIn":{"edge":"left","durationMs":400},"motionOut":{"edge":"right","durationMs":400}}`, CodeOutOfRange},
		"unknown easing":                {`{"scope":"visual","durationMs":500,"keyframes":{"opacity":[{"atMs":0,"value":0,"easing":"wobble"}]}}`, CodeUnknown},
		"x and position together":       {`{"scope":"visual","durationMs":500,"keyframes":{"x":[{"atMs":0,"value":0,"easing":"linear"}],"position":[{"atMs":0,"x":0,"y":0,"easing":"linear"}]}}`, CodeInvalid},
		"duplicate behaviors":           {`{"scope":"visual","durationMs":500,"behaviors":[{"id":"a","kind":"drift","property":"x","rate":0.1},{"id":"a","kind":"drift","property":"y","rate":0.1}]}`, CodeDuplicate},
		"empty keyframes":               {`{"scope":"visual","durationMs":500,"keyframes":{},"motionBlur":true}`, CodeRequired},
	}
	for name, c := range cases {
		err := ValidatePresetAnimation([]byte(c.raw))
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		var invalid *ValidationError
		if !errors.As(err, &invalid) || len(invalid.Issues) != 1 || invalid.Issues[0].Field != FieldAnimation || invalid.Issues[0].Code != c.code {
			t.Errorf("%s: %v, want animation %s", name, err, c.code)
		}
	}
}

func TestRelativeLanesAreShiftedOntoTheBaseTransform(t *testing.T) {
	relative := &ClipKeyframes{
		Keyframes: mediagen.Keyframes{
			X:        []mediagen.Keyframe{{AtMS: 0, Value: -0.1, Easing: mediagen.EaseLinear}},
			Y:        []mediagen.Keyframe{{AtMS: 0, Value: 0.2, Easing: mediagen.EaseLinear}},
			Rotation: []mediagen.Keyframe{{AtMS: 0, Value: 15, Easing: mediagen.EaseLinear}},
			Opacity:  []mediagen.Keyframe{{AtMS: 0, Value: 0.5, Easing: mediagen.EaseLinear}},
			Scale:    []mediagen.Keyframe{{AtMS: 0, Value: 2, Easing: mediagen.EaseLinear}},
		},
		Position: []PositionKeyframe{{AtMS: 0, X: 0.1, Y: -0.1, Easing: mediagen.EaseLinear, Spatial: SpatialSmooth, In: &Tangent{X: -0.2, Y: 0.1}, Out: &Tangent{X: 0.2, Y: -0.1}}},
	}
	absolute := PresetKeyframesAt(relative, mediagen.Transform{X: 0.3, Y: 0.6, Rotation: 10})
	near := func(got, want float64) bool { return math.Abs(got-want) < 1e-9 }
	if !near(absolute.X[0].Value, 0.2) || !near(absolute.Y[0].Value, 0.8) || absolute.Rotation[0].Value != 25 || absolute.Opacity[0].Value != 0.5 || absolute.Scale[0].Value != 2 {
		t.Fatalf("absolute = %+v", absolute.Keyframes)
	}
	position := absolute.Position[0]
	if !near(position.X, 0.4) || !near(position.Y, 0.5) || position.In.X != -0.2 || position.Out.Y != -0.1 {
		t.Fatalf("position = %+v", position)
	}
	if relative.X[0].Value != -0.1 || relative.Position[0].X != 0.1 {
		t.Fatal("the relative keys were changed in place")
	}
	if PresetKeyframesAt(nil, mediagen.Transform{}) != nil {
		t.Fatal("no keys stay no keys")
	}
}

func TestANewPresetCleansItsTextAndValidatesTheAnimation(t *testing.T) {
	animation := json.RawMessage(`{"scope":"visual","durationMs":1000,"motionBlur":true}`)
	p, err := NewAnimationPreset("ws-1", "u-1", "  Meu   giro  ", "  Para títulos  ", animation)
	if err != nil || p.Name != "Meu giro" || p.Description != "Para títulos" || p.BuiltIn {
		t.Fatalf("preset %+v err %v", p, err)
	}
	if _, err := NewAnimationPreset("", "u-1", "x", "", animation); !errors.Is(err, ErrWorkspaceRequired) {
		t.Fatalf("workspace %v", err)
	}
	if _, err := NewAnimationPreset("ws-1", " ", "x", "", animation); !errors.Is(err, ErrCreatorRequired) {
		t.Fatalf("creator %v", err)
	}
	cases := map[string]struct {
		name, description string
		animation         json.RawMessage
		field, code       string
	}{
		"blank name":       {"   ", "", animation, FieldName, CodeRequired},
		"long name":        {strings.Repeat("a", MaxPresetNameRunes+1), "", animation, FieldName, CodeTooLarge},
		"long description": {"ok", strings.Repeat("é", MaxPresetDescriptionRunes+1), animation, FieldDescription, CodeTooLarge},
		"bad animation":    {"ok", "", json.RawMessage(`{"scope":"visual","durationMs":1000}`), FieldAnimation, CodeRequired},
	}
	for label, c := range cases {
		_, err := NewAnimationPreset("ws-1", "u-1", c.name, c.description, c.animation)
		var invalid *ValidationError
		if !errors.As(err, &invalid) || invalid.Codes()[c.field] != c.code {
			t.Errorf("%s: %v", label, err)
		}
	}
	if _, err := NewAnimationPreset("ws-1", "u-1", strings.Repeat("ã", MaxPresetNameRunes), strings.Repeat("ã", MaxPresetDescriptionRunes), animation); err != nil {
		t.Fatalf("limits count runes: %v", err)
	}
}

func TestAPresetChangeRenamesWithoutTouchingTheAnimation(t *testing.T) {
	p, _ := NewAnimationPreset("ws-1", "u-1", "Antes", "Velha", json.RawMessage(`{"scope":"visual","durationMs":1000,"motionBlur":true}`))
	if err := p.Apply(PresetChange{}); err == nil {
		t.Fatal("an empty change was accepted")
	}
	name, description := " Depois ", ""
	if err := p.Apply(PresetChange{Name: &name, Description: &description}); err != nil || p.Name != "Depois" || p.Description != "" {
		t.Fatalf("preset %+v err %v", p, err)
	}
	blank := " "
	if err := p.Apply(PresetChange{Name: &blank, Description: &name}); err == nil || p.Name != "Depois" || p.Description != "" {
		t.Fatalf("a refused change was half applied: %+v", p)
	}
}

func TestThePresetSummaryNamesWhatItCarries(t *testing.T) {
	fixture := loadPresetFixture(t)
	visual := AnimationPreset{Animation: fixture.Valid[0].Animation}
	got, err := visual.Summary()
	if err != nil {
		t.Fatal(err)
	}
	want := PresetSummary{Scope: PresetScopeVisual, DurationMS: 2000, Keys: []string{"opacity", "position"}, Behaviors: []string{"wiggle"}, Entrance: PresetEdgeMotion, Exit: PresetEdgeTransition, Vfx: []string{}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("summary = %+v", got)
	}
	text, _ := AnimationPreset{Animation: fixture.Valid[2].Animation}.Summary()
	if text.Entrance != PresetEdgeText || text.Exit != "" || text.TextAnimators != 1 || len(text.Keys) != 0 {
		t.Fatalf("text summary = %+v", text)
	}
	vfx, _ := AnimationPreset{Animation: fixture.Valid[3].Animation}.Summary()
	if !reflect.DeepEqual(vfx.Vfx, []string{"grain"}) || vfx.MotionBlur {
		t.Fatalf("vfx summary = %+v", vfx)
	}
	if _, err := (AnimationPreset{Animation: json.RawMessage(`{}`)}).Summary(); err == nil {
		t.Fatal("a broken animation was summarized")
	}
}

func TestEveryBuiltinPresetValidatesAndSpeaksEveryLocale(t *testing.T) {
	presets, err := BuiltinPresets()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"builtin.riseIn", "builtin.popIn", "builtin.slideAcross", "builtin.breathe", "builtin.wordCascade"}
	if len(presets) != len(want) {
		t.Fatalf("built-ins = %d", len(presets))
	}
	scopes := map[string]bool{}
	for i, p := range presets {
		if p.ID != want[i] || !p.BuiltIn || !IsBuiltinPresetID(p.ID) {
			t.Fatalf("preset %d = %+v", i, p)
		}
		if err := ValidatePresetAnimation(p.Animation); err != nil {
			t.Fatalf("%s: %v", p.ID, err)
		}
		for _, locale := range BuiltinPresetLocales {
			if p.Names[locale] == "" || p.Descriptions[locale] == "" || strings.ContainsAny(p.Names[locale]+p.Descriptions[locale], string([]rune{0x2013, 0x2014})) {
				t.Fatalf("%s lacks %s text", p.ID, locale)
			}
		}
		summary, _ := p.Summary()
		scopes[summary.Scope] = true
	}
	if !scopes[PresetScopeVisual] || !scopes[PresetScopeText] {
		t.Fatalf("scopes = %v", scopes)
	}
	presets[0].Name = "mutated"
	again, _ := BuiltinPresets()
	if again[0].Name == "mutated" {
		t.Fatal("callers can change the built-ins")
	}
	found, err := BuiltinPreset("builtin.popIn")
	if err != nil || found.Names["en"] != "Pop in" {
		t.Fatalf("found %+v err %v", found, err)
	}
	if _, err := BuiltinPreset("builtin.nope"); !errors.Is(err, ErrPresetNotFound) {
		t.Fatalf("missing %v", err)
	}
}

func TestBrokenBuiltinFilesAreRefused(t *testing.T) {
	entry := func(id, ptName string) string {
		return `{"id":"` + id + `","name":"` + ptName + `","description":"d","names":{"pt":"` + ptName + `","en":"a","es":"a","de":"a"},"descriptions":{"pt":"d","en":"d","es":"d","de":"d"},"animation":{"scope":"visual","durationMs":1000,"motionBlur":true}}`
	}
	if _, err := parseBuiltinPresets([]byte(`{"presets":[` + entry("builtin.ok", "Ok") + `]}`)); err != nil {
		t.Fatalf("a good file was refused: %v", err)
	}
	for name, raw := range map[string]string{
		"no prefix":    `{"presets":[` + entry("ok", "Ok") + `]}`,
		"duplicate":    `{"presets":[` + entry("builtin.ok", "Ok") + `,` + entry("builtin.ok", "Ok") + `]}`,
		"blank name":   `{"presets":[` + entry("builtin.ok", " ") + `]}`,
		"unknown key":  `{"presets":[],"extra":1}`,
		"missing lang": `{"presets":[{"id":"builtin.ok","name":"Ok","description":"d","names":{"pt":"Ok"},"descriptions":{"pt":"d"},"animation":{"scope":"visual","durationMs":1000,"motionBlur":true}}]}`,
	} {
		if _, err := parseBuiltinPresets([]byte(raw)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
