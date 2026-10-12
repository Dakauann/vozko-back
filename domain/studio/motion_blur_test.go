package studio

import (
	"encoding/json"
	"os"
	"testing"
)

func shutter(value float64) *float64 {
	return &value
}

func TestTheEditorMotionBlurDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_motion_blur.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindVideo, raw); err != nil {
		t.Fatalf("the editor saved a motion blurred document the backend refuses: %v", err)
	}
	var doc VideoDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Canvas.ShutterAngle == nil || *doc.Canvas.ShutterAngle != 270 || doc.Canvas.ShutterPhase == nil || *doc.Canvas.ShutterPhase != -135 {
		t.Fatalf("the shutter was lost: %+v", doc.Canvas)
	}
	for _, track := range doc.Tracks[:3] {
		if !track.Clips[0].MotionBlur {
			t.Fatalf("clip %s lost its motion blur switch", track.Clips[0].ID)
		}
	}
}

func TestMotionBlurIsOnlyForClipsWithPictures(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[0].Clips[0].MotionBlur = true
	doc.Tracks[0].Clips[1].MotionBlur = true
	doc.Tracks[1].Clips[0].MotionBlur = true
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("motion blur on image, video and overlay clips was refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"a sound clip": func(d *VideoDocument) { d.Tracks[2].Clips[0].MotionBlur = true },
		"a null clip":  func(d *VideoDocument) { c := nullClip(); c.MotionBlur = true; withNullTrack(d, c) },
	}
	for name, change := range cases {
		d := videoDoc()
		change(&d)
		if code := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks]; code != CodeInvalid {
			t.Errorf("%s: got %q", name, code)
		}
	}
}

func TestTheShutterStaysInsideATurn(t *testing.T) {
	for name, canvas := range map[string]VideoCanvas{
		"a closed shutter":         {ShutterAngle: shutter(MinShutterAngle)},
		"the widest shutter":       {ShutterAngle: shutter(MaxShutterAngle), ShutterPhase: shutter(MinShutterPhase)},
		"the latest phase":         {ShutterPhase: shutter(MaxShutterPhase)},
		"the defaults written out": {ShutterAngle: shutter(DefaultShutterAngle), ShutterPhase: shutter(DefaultShutterPhase)},
	} {
		d := videoDoc()
		d.Canvas.ShutterAngle, d.Canvas.ShutterPhase = canvas.ShutterAngle, canvas.ShutterPhase
		if err := ValidateDocument(KindVideo, mustJSON(t, d)); err != nil {
			t.Errorf("%s was refused: %v", name, err)
		}
	}
	for name, canvas := range map[string]VideoCanvas{
		"a negative angle":        {ShutterAngle: shutter(-1)},
		"an angle past two turns": {ShutterAngle: shutter(MaxShutterAngle + 1)},
		"a phase before a turn":   {ShutterPhase: shutter(MinShutterPhase - 1)},
		"a phase after a turn":    {ShutterPhase: shutter(MaxShutterPhase + 1)},
	} {
		d := videoDoc()
		d.Canvas.ShutterAngle, d.Canvas.ShutterPhase = canvas.ShutterAngle, canvas.ShutterPhase
		if code := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldCanvas]; code != CodeOutOfRange {
			t.Errorf("%s: got %q", name, code)
		}
	}
}

func TestTheShutterDefaultsMatchTheEditor(t *testing.T) {
	if DefaultShutterAngle != 180 || DefaultShutterPhase != -90 || MinShutterAngle != 0 || MaxShutterAngle != 720 || MinShutterPhase != -360 || MaxShutterPhase != 360 {
		t.Fatal("the shutter limits drifted from the editor (motion-blur.ts)")
	}
}
