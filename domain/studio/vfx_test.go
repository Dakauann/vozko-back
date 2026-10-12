package studio

import (
	"encoding/json"
	"os"
	"testing"

	"vozko/domain/mediagen"
)

type vfxFixture struct {
	Specs map[string]struct {
		Params map[string][4]any `json:"params"`
		Colors map[string]string `json:"colors"`
	} `json:"specs"`
	Valid json.RawMessage `json:"valid"`
}

func readVfxFixture(t *testing.T) vfxFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/editor_vfx.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture vfxFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestTheVfxSpecsMatchTheEditor(t *testing.T) {
	fixture := readVfxFixture(t)
	if len(fixture.Specs) != len(vfxKinds) {
		t.Fatalf("the editor lists %d effect kinds, the backend %d", len(fixture.Specs), len(vfxKinds))
	}
	for name, editor := range fixture.Specs {
		kind, known := vfxKinds[name]
		if !known {
			t.Fatalf("the backend does not know the effect %s", name)
		}
		if len(editor.Params) != len(kind.params) || len(editor.Colors) != len(kind.colors) {
			t.Fatalf("%s: the parameter or color lists differ", name)
		}
		for param, row := range editor.Params {
			spec, ok := kind.params[param]
			if !ok || row[0].(float64) != spec.low || row[1].(float64) != spec.high || row[2].(float64) != spec.fallback || row[3].(bool) != spec.keyed {
				t.Errorf("%s.%s differs: editor %v, backend %+v", name, param, row, spec)
			}
		}
		for color, fallback := range editor.Colors {
			if kind.colors[color] != fallback {
				t.Errorf("%s.%s differs: editor %s, backend %s", name, color, fallback, kind.colors[color])
			}
		}
	}
}

func TestTheEditorVfxDocumentPassesTheContract(t *testing.T) {
	if err := ValidateDocument(KindVideo, readVfxFixture(t).Valid); err != nil {
		t.Fatalf("the editor saved effects the backend refuses: %v", err)
	}
}

func float(v float64) *float64 { return &v }

func effectClip(vfx ...Vfx) Clip {
	return Clip{ID: "fx", Type: ClipEffect, StartMS: 0, DurationMS: 1_000, Volume: 1, Transform: mediagen.FullFrame(), Vfx: vfx}
}

func withEffectTrack(d *VideoDocument, clip Clip) {
	d.Tracks = append(d.Tracks, Track{ID: "effects", Kind: TrackVisual, Clips: []Clip{clip}})
}

func TestEffectsOnClipsAndEffectClips(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[0].Clips[0].Vfx = []Vfx{{ID: "g", Kind: "grain", Seed: 4, Params: map[string]*float64{"amount": float(0)}}}
	withEffectTrack(&doc, effectClip(Vfx{ID: "l", Kind: "lightLeak", Seed: 1, Keyframes: map[string][]mediagen.Keyframe{"amount": {{AtMS: 0, Value: 0, Easing: mediagen.EaseLinear}, {AtMS: 500, Value: 1, Easing: mediagen.EaseLinear}}}}))
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("effects refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"an effect on a song": func(d *VideoDocument) { d.Tracks[2].Clips[0].Vfx = []Vfx{{ID: "g", Kind: "grain"}} },
		"an unknown kind":     func(d *VideoDocument) { d.Tracks[0].Clips[0].Vfx = []Vfx{{ID: "g", Kind: "smoke"}} },
		"two effects with one id": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Vfx = []Vfx{{ID: "g", Kind: "grain"}, {ID: "g", Kind: "vignette"}}
		},
		"a parameter of another kind": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Vfx = []Vfx{{ID: "g", Kind: "grain", Params: map[string]*float64{"ghosts": float(0.2)}}}
		},
		"a value out of range": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Vfx = []Vfx{{ID: "g", Kind: "grain", Params: map[string]*float64{"amount": float(2)}}}
		},
		"a seed out of range": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Vfx = []Vfx{{ID: "g", Kind: "grain", Seed: MaxVfxSeed + 1}}
		},
		"a short color": func(d *VideoDocument) {
			short := "#fff"
			d.Tracks[0].Clips[0].Vfx = []Vfx{{ID: "v", Kind: "vignette", Colors: map[string]*string{"color": &short}}}
		},
		"a color the kind does not paint": func(d *VideoDocument) {
			red := "#ff0000"
			d.Tracks[0].Clips[0].Vfx = []Vfx{{ID: "g", Kind: "grain", Colors: map[string]*string{"color": &red}}}
		},
		"a key on a pace parameter": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Vfx = []Vfx{{ID: "p", Kind: "particles", Keyframes: map[string][]mediagen.Keyframe{"speed": {{AtMS: 0, Value: 1, Easing: mediagen.EaseLinear}}}}}
		},
		"an effect clip with media":  func(d *VideoDocument) { c := effectClip(); c.AssetID = "m-1"; withEffectTrack(d, c) },
		"an effect clip off center":  func(d *VideoDocument) { c := effectClip(); c.Transform.X = 0.4; withEffectTrack(d, c) },
		"an effect clip with a trim": func(d *VideoDocument) { c := effectClip(); c.TrimInMS = 100; withEffectTrack(d, c) },
		"an effect clip on a sound track": func(d *VideoDocument) {
			d.Tracks[2].Clips = append(d.Tracks[2].Clips, Clip{ID: "x", Type: ClipEffect, StartMS: 0, DurationMS: 500, Volume: 1, Transform: mediagen.FullFrame()})
		},
	}
	for name, change := range cases {
		d := videoDoc()
		change(&d)
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks] == "" {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestEffectParametersAcceptNullAsAbsent(t *testing.T) {
	doc := videoDoc()
	withEffectTrack(&doc, effectClip(Vfx{ID: "g", Kind: "grain", Params: map[string]*float64{"amount": nil}}))
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a null parameter was refused: %v", err)
	}
}
