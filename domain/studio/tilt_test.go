package studio

import (
	"encoding/json"
	"os"
	"testing"

	"vozko/domain/mediagen"
)

func angle(value float64) *float64 {
	return &value
}

func word(value string) *string {
	return &value
}

func tiltKeys(values ...float64) []mediagen.Keyframe {
	frames := make([]mediagen.Keyframe, len(values))
	for i, value := range values {
		frames[i] = mediagen.Keyframe{AtMS: int64(i * 500), Value: value, Easing: mediagen.EaseLinear}
	}
	return frames
}

func TestTheEditorTiltDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_tilt.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindVideo, raw); err != nil {
		t.Fatalf("the editor saved a document turned in depth that the backend refuses: %v", err)
	}
	var doc VideoDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Canvas.Perspective == nil || *doc.Canvas.Perspective != 0 {
		t.Fatalf("a flat lens (0) must stay apart from the default: %+v", doc.Canvas)
	}
	card := doc.Tracks[1].Clips[0]
	if card.RotationY != 45 || len(card.Keyframes.RotationX) != 2 {
		t.Fatalf("the card lost its turn: %+v", card)
	}
	title := doc.Tracks[3].Clips[0]
	properties := title.TextAnimators[0].Properties
	if title.RotationX != -15 || properties.RotationX == nil || *properties.RotationX != 90 || properties.RotationY == nil || *properties.RotationY != -45 {
		t.Fatalf("the title lost its letter flip: %+v", title)
	}
	again, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindVideo, again); err != nil {
		t.Fatalf("the round trip broke the document: %v", err)
	}
}

func TestOnlyClipsWithPicturesTurnInDepth(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[0].Clips[0].RotationY = 45
	doc.Tracks[0].Clips[1].Keyframes = &ClipKeyframes{TiltKeyframes: TiltKeyframes{RotationX: tiltKeys(0, 90)}}
	doc.Tracks[1].Clips[0].RotationX = -3_600
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("turns on image, video and overlay clips were refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"a sound clip": func(d *VideoDocument) { d.Tracks[2].Clips[0].RotationY = 10 },
		"a null clip":  func(d *VideoDocument) { c := nullClip(); c.RotationX = 10; withNullTrack(d, c) },
		"keys on a null clip": func(d *VideoDocument) {
			c := nullClip()
			c.Keyframes = &ClipKeyframes{TiltKeyframes: TiltKeyframes{RotationY: tiltKeys(0, 10)}}
			withNullTrack(d, c)
		},
		"an effect clip": func(d *VideoDocument) { c := effectClip(); c.RotationY = 10; withEffectTrack(d, c) },
		"keys on a sound clip": func(d *VideoDocument) {
			d.Tracks[2].Clips[0].Keyframes = &ClipKeyframes{TiltKeyframes: TiltKeyframes{RotationX: tiltKeys(0, 10)}}
		},
	}
	for name, change := range cases {
		d := videoDoc()
		change(&d)
		if code := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks]; code != CodeInvalid {
			t.Errorf("%s: got %q", name, code)
		}
	}
}

func TestTurnsStayInsideTheKeyRange(t *testing.T) {
	for name, change := range map[string]func(*VideoDocument){
		"a static turn past ten turns": func(d *VideoDocument) { d.Tracks[0].Clips[0].RotationX = MaxTilt + 1 },
		"a key past ten turns": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Keyframes = &ClipKeyframes{TiltKeyframes: TiltKeyframes{RotationY: tiltKeys(0, -MaxTilt-1)}}
		},
		"a letter flip past ten turns": func(d *VideoDocument) {
			d.Tracks[1].Clips[0].TextAnimators = []TextUnitAnimator{{ID: "a", Unit: "char", Properties: &AnimatorProperties{RotationY: angle(MaxAnimatorRotation + 1)}, Selector: &TextSelector{Kind: SelectorRange, Units: word("percent"), Start: angle(0), End: angle(100), Offset: angle(0), Order: word("forward"), Shape: word("square"), EaseHigh: angle(0), EaseLow: angle(0), Amount: 100}}}
		},
	} {
		d := videoDoc()
		change(&d)
		if code := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks]; code != CodeOutOfRange {
			t.Errorf("%s: got %q", name, code)
		}
	}
}

func TestThePerspectiveStaysBetweenFlatAndWide(t *testing.T) {
	for _, value := range []float64{MinPerspective, DefaultPerspective, MaxPerspective} {
		d := videoDoc()
		d.Canvas.Perspective = angle(value)
		if err := ValidateDocument(KindVideo, mustJSON(t, d)); err != nil {
			t.Errorf("perspective %g was refused: %v", value, err)
		}
	}
	for _, value := range []float64{-1, MaxPerspective + 1} {
		d := videoDoc()
		d.Canvas.Perspective = angle(value)
		if code := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldCanvas]; code != CodeOutOfRange {
			t.Errorf("perspective %g: got %q", value, code)
		}
	}
}

func TestTheTiltLimitsMatchTheEditor(t *testing.T) {
	if MaxTilt != 3600 || MinPerspective != 0 || MaxPerspective != 120 || DefaultPerspective != 40 {
		t.Fatal("the tilt limits drifted from the editor (tilt-properties.ts, perspective.ts)")
	}
}
