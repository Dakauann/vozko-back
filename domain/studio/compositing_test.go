package studio

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"vozko/domain/mediagen"
)

type compositingFixture struct {
	BlendModes    []string              `json:"blendModes"`
	LumaModes     []string              `json:"lumaModes"`
	LumaRanges    map[string][2]float64 `json:"lumaRanges"`
	MatteChannels []string              `json:"matteChannels"`
	Valid         json.RawMessage       `json:"valid"`
}

func readCompositingFixture(t *testing.T) compositingFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/editor_compositing.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture compositingFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestTheCompositingListsMatchTheEditor(t *testing.T) {
	fixture := readCompositingFixture(t)
	if !reflect.DeepEqual(fixture.BlendModes, BlendModes()) {
		t.Fatalf("blend modes differ: editor %v, backend %v", fixture.BlendModes, BlendModes())
	}
	if !reflect.DeepEqual(fixture.LumaModes, LumaKeyModes()) {
		t.Fatalf("luma key modes differ: editor %v, backend %v", fixture.LumaModes, LumaKeyModes())
	}
	if !reflect.DeepEqual(fixture.MatteChannels, MatteChannels()) {
		t.Fatalf("matte channels differ: editor %v, backend %v", fixture.MatteChannels, MatteChannels())
	}
	want := map[string][2]float64{"threshold": {MinLumaThreshold, MaxLumaValue}, "softness": {MinLumaSoftness, MaxLumaValue}}
	if !reflect.DeepEqual(fixture.LumaRanges, want) {
		t.Fatalf("luma ranges differ: editor %v, backend %v", fixture.LumaRanges, want)
	}
}

func TestTheEditorCompositingDocumentPassesTheContract(t *testing.T) {
	if err := ValidateDocument(KindVideo, readCompositingFixture(t).Valid); err != nil {
		t.Fatalf("the editor saved compositing the backend refuses: %v", err)
	}
}

func darkKey() *LumaKey {
	return &LumaKey{Mode: LumaDark, Threshold: 0.1, Softness: 0.1}
}

func TestLumaKeyOnVideoAndImageClips(t *testing.T) {
	doc := videoDoc()
	keyed := darkKey()
	keyed.Keyframes = &LumaKeyframes{Threshold: []mediagen.Keyframe{{AtMS: 0, Value: 0, Easing: mediagen.EaseLinear}, {AtMS: 1000, Value: 0.4, Easing: mediagen.EaseLinear}}}
	doc.Tracks[0].Clips[0].LumaKey = darkKey()
	doc.Tracks[0].Clips[1].LumaKey = keyed
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("luma key refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"a key on a text":        func(d *VideoDocument) { d.Tracks[1].Clips[0].LumaKey = darkKey() },
		"a key on a song":        func(d *VideoDocument) { d.Tracks[2].Clips[0].LumaKey = darkKey() },
		"an unknown mode":        func(d *VideoDocument) { k := darkKey(); k.Mode = "gray"; d.Tracks[0].Clips[0].LumaKey = k },
		"a threshold past white": func(d *VideoDocument) { k := darkKey(); k.Threshold = 1.5; d.Tracks[0].Clips[0].LumaKey = k },
		"a zero softness":        func(d *VideoDocument) { k := darkKey(); k.Softness = 0; d.Tracks[0].Clips[0].LumaKey = k },
		"a key out of range": func(d *VideoDocument) {
			k := darkKey()
			k.Keyframes = &LumaKeyframes{Softness: []mediagen.Keyframe{{AtMS: 0, Value: 2, Easing: mediagen.EaseLinear}}}
			d.Tracks[0].Clips[0].LumaKey = k
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

func TestMatteChannelsAndInvert(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[1].Clips[0].Matte = true
	doc.Tracks[1].Clips[0].MatteChannel = MatteLuma
	doc.Tracks[1].Clips[0].MatteInvert = true
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a luma inverted matte refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"a channel without the matte": func(d *VideoDocument) { d.Tracks[1].Clips[0].MatteChannel = MatteLuma },
		"invert without the matte":    func(d *VideoDocument) { d.Tracks[1].Clips[0].MatteInvert = true },
		"an unknown channel": func(d *VideoDocument) {
			d.Tracks[1].Clips[0].Matte = true
			d.Tracks[1].Clips[0].MatteChannel = "red"
		},
		"a matte channel on a song": func(d *VideoDocument) { d.Tracks[2].Clips[0].MatteChannel = MatteLuma },
	}
	for name, change := range cases {
		d := videoDoc()
		change(&d)
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks] == "" {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestBlendModesOnMediaClips(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[0].Clips[0].BlendMode = "add"
	doc.Tracks[0].Clips[1].BlendMode = "screen"
	doc.Tracks[1].Clips[0].Layer.BlendMode = "add"
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("blend modes on media refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"a clip mode on a text": func(d *VideoDocument) { d.Tracks[1].Clips[0].BlendMode = "add" },
		"a mode on a song":      func(d *VideoDocument) { d.Tracks[2].Clips[0].BlendMode = "add" },
		"an unknown mode":       func(d *VideoDocument) { d.Tracks[0].Clips[0].BlendMode = "glow" },
		"a mode on an effect clip": func(d *VideoDocument) {
			c := effectClip()
			c.BlendMode = "add"
			withEffectTrack(d, c)
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
