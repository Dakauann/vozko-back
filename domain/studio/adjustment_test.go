package studio

import (
	"encoding/json"
	"os"
	"testing"

	"vozko/domain/mediagen"
)

func adjustmentKeys(low, high float64) []mediagen.Keyframe {
	return []mediagen.Keyframe{{AtMS: 0, Value: low, Easing: mediagen.EaseLinear}, {AtMS: 500, Value: high, Easing: mediagen.EaseLinear}}
}

func TestTheEditorAdjustmentDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_adjustment.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindVideo, raw); err != nil {
		t.Fatalf("the editor saved an adjustment clip the backend refuses: %v", err)
	}
	var doc VideoDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	fx := doc.Tracks[1].Clips[0]
	switch {
	case fx.Camera == nil || fx.Camera.Zoom != 1.2 || fx.Camera.Rotation != 3:
		t.Fatalf("the camera was lost: %+v", fx.Camera)
	case fx.Keyframes == nil || len(fx.Keyframes.CameraZoom) != 2 || len(fx.Keyframes.Exposure) != 2 || len(fx.Keyframes.Blur) != 2:
		t.Fatalf("the adjustment keys were lost: %+v", fx.Keyframes)
	case fx.Color == nil || fx.Color.Lut == nil || fx.Color.Curves == nil || fx.GradientMap == nil || len(fx.Masks) != 1 || !fx.MotionBlur || fx.Blur != 2:
		t.Fatalf("the adjustment lost part of its stack: %+v", fx)
	}
}

func TestAdjustmentClipsCarryTheirStackStrictly(t *testing.T) {
	valid := map[string]func(*Clip){
		"color": func(c *Clip) { c.Color = &ColorGrade{Contrast: 20, Lut: &GradeLut{AssetID: "look"}} },
		"blur":  func(c *Clip) { c.Blur = MaxClipBlur },
		"a camera": func(c *Clip) {
			c.Camera = &ClipCamera{PanX: -MaxCameraPan, PanY: MaxCameraPan, Zoom: MinCameraZoom, Rotation: -MaxCameraRotation}
		},
		"camera keys": func(c *Clip) {
			c.Keyframes = &ClipKeyframes{CameraKeyframes: CameraKeyframes{CameraZoom: adjustmentKeys(1, MaxCameraZoom)}}
		},
		"grade keys": func(c *Clip) {
			c.Keyframes = &ClipKeyframes{GradeKeyframes: GradeKeyframes{Exposure: adjustmentKeys(-1, 1)}}
		},
		"blur keys": func(c *Clip) { c.Keyframes = &ClipKeyframes{Blur: adjustmentKeys(0, 20)} },
		"a mask": func(c *Clip) {
			c.Masks = []Mask{{ID: "m", Shape: "ellipse", Mode: "add", X: 0.5, Y: 0.5, W: 0.5, H: 0.5, Opacity: 1}}
		},
		"motion blur": func(c *Clip) { c.MotionBlur = true },
	}
	for name, change := range valid {
		d := videoDoc()
		fx := effectClip()
		change(&fx)
		withEffectTrack(&d, fx)
		if err := ValidateDocument(KindVideo, mustJSON(t, d)); err != nil {
			t.Errorf("%s on an adjustment clip was refused: %v", name, err)
		}
	}
	refused := map[string]struct {
		change func(*Clip)
		code   string
	}{
		"a zoom too small":    {func(c *Clip) { c.Camera = &ClipCamera{Zoom: MinCameraZoom / 2} }, CodeOutOfRange},
		"a pan off the range": {func(c *Clip) { c.Camera = &ClipCamera{PanX: MaxCameraPan + 0.1, Zoom: 1} }, CodeOutOfRange},
		"a zoom key too large": {func(c *Clip) {
			c.Keyframes = &ClipKeyframes{CameraKeyframes: CameraKeyframes{CameraZoom: adjustmentKeys(1, MaxCameraZoom+1)}}
		}, CodeOutOfRange},
		"blur past the range": {func(c *Clip) { c.Blur = MaxClipBlur + 1 }, CodeOutOfRange},
		"a position key": {func(c *Clip) {
			c.Keyframes = &ClipKeyframes{Keyframes: mediagen.Keyframes{X: adjustmentKeys(0.2, 0.8)}}
		}, CodeInvalid},
		"a volume key": {func(c *Clip) { c.Keyframes = &ClipKeyframes{GainDB: adjustmentKeys(-6, 0)} }, CodeInvalid},
		"a bad grade":  {func(c *Clip) { c.Color = &ColorGrade{Exposure: MaxExposureStops + 1} }, CodeOutOfRange},
		"media":        {func(c *Clip) { c.AssetID = "m-1" }, CodeInvalid},
		"a chroma key": {func(c *Clip) {
			c.ChromaKey = &ChromaKey{Color: "#00ff00", Similarity: 0.4, Smoothness: 0.1, Spill: 0.1}
		}, CodeInvalid},
	}
	for name, test := range refused {
		d := videoDoc()
		fx := effectClip()
		test.change(&fx)
		withEffectTrack(&d, fx)
		if code := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks]; code != test.code {
			t.Errorf("%s: got %q, want %q", name, code, test.code)
		}
	}
}

func TestOnlyAdjustmentClipsHaveACamera(t *testing.T) {
	cases := map[string]func(*VideoDocument){
		"a camera on an image": func(d *VideoDocument) { d.Tracks[0].Clips[0].Camera = &ClipCamera{Zoom: 2} },
		"camera keys on an image": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Keyframes = &ClipKeyframes{CameraKeyframes: CameraKeyframes{CameraZoom: adjustmentKeys(1, 2)}}
		},
		"a camera on a sound clip": func(d *VideoDocument) { d.Tracks[2].Clips[0].Camera = &ClipCamera{Zoom: 2} },
	}
	for name, change := range cases {
		d := videoDoc()
		change(&d)
		if code := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks]; code != CodeInvalid {
			t.Errorf("%s: got %q", name, code)
		}
	}
}
