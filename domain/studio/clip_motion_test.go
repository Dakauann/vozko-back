package studio

import (
	"encoding/json"
	"os"
	"testing"

	"vozko/domain/mediagen"
)

func scaleKeys(values ...float64) []mediagen.Keyframe {
	frames := make([]mediagen.Keyframe, len(values))
	for i, v := range values {
		frames[i] = mediagen.Keyframe{AtMS: int64(i * 500), Value: v, Easing: mediagen.EaseLinear}
	}
	return frames
}

func pathKeys() []PositionKeyframe {
	return []PositionKeyframe{
		{AtMS: 0, X: 0.2, Y: 0.8, Easing: mediagen.EaseInOut},
		{AtMS: 800, X: 0.5, Y: 0.3, Easing: mediagen.EaseLinear, Spatial: SpatialSmooth, In: &Tangent{X: -0.1, Y: 0}, Out: &Tangent{X: 0.1, Y: 0}},
		{AtMS: 1600, X: 0.8, Y: 0.7, Easing: mediagen.EaseLinear, Spatial: SpatialLinear},
	}
}

func TestTheEditorMotionDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_motion.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindVideo, raw); err != nil {
		t.Fatalf("the editor saved a motion document the backend refuses: %v", err)
	}
	var doc VideoDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	plane := doc.Tracks[1].Clips[0]
	if plane.Anchor == nil || plane.Anchor.X != 0.25 || !plane.AutoOrient || !plane.FlipY || len(plane.Keyframes.Position) != 5 || plane.Keyframes.Position[1].In == nil {
		t.Fatalf("motion fields lost: %+v", plane)
	}
}

func TestAnchorsFlipsAndOrientationBelongToVisualClips(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[0].Clips[0].Anchor = &Anchor{X: -1, Y: 2}
	doc.Tracks[0].Clips[0].FlipX = true
	doc.Tracks[1].Clips[0].FlipY = true
	doc.Tracks[1].Clips[0].AutoOrient = true
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("visual motion fields refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"an anchor far outside the box": func(d *VideoDocument) { d.Tracks[0].Clips[0].Anchor = &Anchor{X: 2.5, Y: 0.5} },
		"an anchor on a sound":          func(d *VideoDocument) { d.Tracks[2].Clips[0].Anchor = &Anchor{X: 0, Y: 0} },
		"a flipped sound":               func(d *VideoDocument) { d.Tracks[2].Clips[0].FlipX = true },
		"an oriented sound":             func(d *VideoDocument) { d.Tracks[2].Clips[0].AutoOrient = true },
	}
	for name, mutate := range cases {
		bad := videoDoc()
		mutate(&bad)
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, bad)))[FieldTracks] == "" {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestScaleAxesAnimateSeparatelyButNeverBesideUniformScale(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[1].Clips[0].Keyframes = &ClipKeyframes{ScaleX: scaleKeys(1, 1.5)}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("one animated axis refused: %v", err)
	}
	doc.Tracks[1].Clips[0].Keyframes.ScaleY = scaleKeys(1, 0.5, 2)
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("both axes refused: %v", err)
	}
	cases := map[string]*ClipKeyframes{
		"scale beside scaleX":   {Keyframes: mediagen.Keyframes{Scale: scaleKeys(1, 2)}, ScaleX: scaleKeys(1, 2)},
		"scale beside scaleY":   {Keyframes: mediagen.Keyframes{Scale: scaleKeys(1, 2)}, ScaleY: scaleKeys(1, 2)},
		"an axis below range":   {ScaleX: scaleKeys(1, 0.01)},
		"an axis above range":   {ScaleY: scaleKeys(1, 6)},
		"a box past four wide":  {ScaleX: scaleKeys(1, 5.1)},
		"keys out of order":     {ScaleX: []mediagen.Keyframe{{AtMS: 500, Value: 1, Easing: mediagen.EaseLinear}, {AtMS: 100, Value: 2, Easing: mediagen.EaseLinear}}},
		"an unknown axis curve": {ScaleY: []mediagen.Keyframe{{AtMS: 0, Value: 1, Easing: "wobble"}}},
	}
	for name, k := range cases {
		bad := videoDoc()
		bad.Tracks[1].Clips[0].Keyframes = k
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, bad)))[FieldTracks] == "" {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestTheAxisBoxRuleCountsEachAxisAgainstItsOwnSide(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[1].Clips[0].Keyframes = &ClipKeyframes{ScaleY: scaleKeys(1, 5)}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a short box growing five times tall was refused: %v", err)
	}
	doc.Tracks[0].Clips[0].Keyframes = &ClipKeyframes{ScaleX: scaleKeys(1, 4.5)}
	if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, doc)))[FieldTracks] != CodeOutOfRange {
		t.Fatal("a full frame growing past four frames wide passed the limit")
	}
}

func TestAPositionPathCarriesSpatialModesAndHandles(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[1].Clips[0].Keyframes = &ClipKeyframes{Position: pathKeys()}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a curved path refused: %v", err)
	}
	broken := map[string]func([]PositionKeyframe){
		"an unknown spatial mode":    func(k []PositionKeyframe) { k[0].Spatial = "bendy" },
		"a smooth key without out":   func(k []PositionKeyframe) { k[1].Out = nil },
		"a free key without in":      func(k []PositionKeyframe) { k[1].Spatial, k[1].In = SpatialFree, nil },
		"handles on an auto key":     func(k []PositionKeyframe) { k[0].Out = &Tangent{X: 0.1} },
		"handles on a linear key":    func(k []PositionKeyframe) { k[2].In = &Tangent{X: 0.1} },
		"a handle too long":          func(k []PositionKeyframe) { k[1].Out = &Tangent{X: 2.5} },
		"a position off the stage":   func(k []PositionKeyframe) { k[0].X = 2.1 },
		"keys out of order":          func(k []PositionKeyframe) { k[2].AtMS = 100 },
		"an unknown curve":           func(k []PositionKeyframe) { k[0].Easing = "wobble" },
		"a key past the video limit": func(k []PositionKeyframe) { k[2].AtMS = MaxVideoMS + 1 },
	}
	for name, mutate := range broken {
		bad := videoDoc()
		frames := pathKeys()
		mutate(frames)
		bad.Tracks[1].Clips[0].Keyframes = &ClipKeyframes{Position: frames}
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, bad)))[FieldTracks] == "" {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestAPathReplacesTheSeparateXAndYLanes(t *testing.T) {
	for name, k := range map[string]*ClipKeyframes{
		"path beside x": {Keyframes: mediagen.Keyframes{X: scaleKeys(0.2, 0.4)}, Position: pathKeys()},
		"path beside y": {Keyframes: mediagen.Keyframes{Y: scaleKeys(0.2, 0.4)}, Position: pathKeys()},
	} {
		bad := videoDoc()
		bad.Tracks[1].Clips[0].Keyframes = k
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, bad)))[FieldTracks] != CodeInvalid {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestASoundTakesNoMotionLanes(t *testing.T) {
	for name, k := range map[string]*ClipKeyframes{
		"an axis scale": {ScaleX: scaleKeys(1, 2)},
		"a path":        {Position: pathKeys()},
	} {
		bad := videoDoc()
		bad.Tracks[2].Clips[0].Keyframes = k
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, bad)))[FieldTracks] == "" {
			t.Fatalf("%s on a sound was accepted", name)
		}
	}
}
