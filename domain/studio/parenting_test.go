package studio

import (
	"encoding/json"
	"os"
	"testing"

	"vozko/domain/mediagen"
)

func nullClip() Clip {
	return Clip{ID: "rig", Type: ClipNull, DurationMS: 3_000, Volume: 1, Transform: mediagen.Transform{X: 0.5, Y: 0.5, W: 0.1, H: 0.1, Opacity: 1}}
}

func withNullTrack(d *VideoDocument, clip Clip) {
	d.Tracks = append(d.Tracks, Track{ID: "controls", Kind: TrackVisual, Clips: []Clip{clip}})
}

func TestTheEditorParentDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_parent.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindVideo, raw); err != nil {
		t.Fatalf("the editor saved a parented document the backend refuses: %v", err)
	}
	var doc VideoDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	photo, title, rig := doc.Tracks[0].Clips[0], doc.Tracks[1].Clips[0], doc.Tracks[2].Clips[0]
	if photo.ParentID != "rig" || title.ParentID != "photo" || rig.Type != ClipNull || rig.ParentID != "" {
		t.Fatalf("parent fields lost: photo %q, title %q, rig %q %q", photo.ParentID, title.ParentID, rig.Type, rig.ParentID)
	}
	if photo.Transform.X != -0.2 || title.Transform.Y != 1.3 || rig.Keyframes == nil || len(rig.Keyframes.Position) != 2 {
		t.Fatalf("parented placement lost: %+v %+v %+v", photo.Transform, title.Transform, rig.Keyframes)
	}
}

func TestParentsMustFormATreeOfVisualClips(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[1].Clips[0].ParentID = "c1"
	withNullTrack(&doc, nullClip())
	doc.Tracks[0].Clips[0].ParentID = "rig"
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a parent chain across tracks was refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"a missing parent": func(d *VideoDocument) { d.Tracks[0].Clips[0].ParentID = "ghost" },
		"two clips parenting each other": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].ParentID = "o1"
			d.Tracks[1].Clips[0].ParentID = "c1"
		},
		"a self parent":         func(d *VideoDocument) { d.Tracks[0].Clips[0].ParentID = "c1" },
		"an effect clip parent": func(d *VideoDocument) { withEffectTrack(d, effectClip()); d.Tracks[0].Clips[0].ParentID = "fx" },
		"an audio child":        func(d *VideoDocument) { d.Tracks[2].Clips[0].ParentID = "c1" },
		"an audio parent":       func(d *VideoDocument) { d.Tracks[0].Clips[0].ParentID = "a1" },
		"an effect clip child":  func(d *VideoDocument) { c := effectClip(); c.ParentID = "c1"; withEffectTrack(d, c) },
		"a longer loop of three": func(d *VideoDocument) {
			withNullTrack(d, nullClip())
			d.Tracks[0].Clips[0].ParentID = "o1"
			d.Tracks[1].Clips[0].ParentID = "rig"
			d.Tracks[4].Clips[0].ParentID = "c1"
		},
		"a chain into a lost one": func(d *VideoDocument) {
			withNullTrack(d, nullClip())
			d.Tracks[0].Clips[0].ParentID = "rig"
			d.Tracks[4].Clips[0].ParentID = "ghost"
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

func TestOffFramePositionsNeedAParent(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[0].Clips[0].ParentID = "o1"
	doc.Tracks[0].Clips[0].Transform.X = -0.4
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a parented clip off frame was refused: %v", err)
	}
	loose := videoDoc()
	loose.Tracks[0].Clips[0].Transform.X = -0.4
	if code := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, loose)))[FieldTracks]; code != CodeOutOfRange {
		t.Fatalf("a loose clip off frame: got %q", code)
	}
	far := videoDoc()
	far.Tracks[0].Clips[0].ParentID = "o1"
	far.Tracks[0].Clips[0].Transform.X = 2.4
	if code := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, far)))[FieldTracks]; code != CodeOutOfRange {
		t.Fatalf("a parented clip past the key range: got %q", code)
	}
}

func TestNullClipsOnlyCarryPlacementAndMotion(t *testing.T) {
	doc := videoDoc()
	rig := nullClip()
	rig.Anchor = &Anchor{X: 0.5, Y: 1}
	rig.AutoOrient = true
	rig.Keyframes = &ClipKeyframes{Keyframes: mediagen.Keyframes{Scale: scaleKeys(1, 1.5), Rotation: scaleKeys(0, 45)}, Position: pathKeys()}
	withNullTrack(&doc, rig)
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a moving null was refused: %v", err)
	}
	axes := videoDoc()
	stretched := nullClip()
	stretched.Keyframes = &ClipKeyframes{Keyframes: mediagen.Keyframes{X: scaleKeys(0.2, 0.8), Y: scaleKeys(0.3, 0.6)}, ScaleX: scaleKeys(1, 2), ScaleY: scaleKeys(1, 0.5)}
	withNullTrack(&axes, stretched)
	if err := ValidateDocument(KindVideo, mustJSON(t, axes)); err != nil {
		t.Fatalf("a null moving on its axes was refused: %v", err)
	}
	cases := map[string]func(*Clip){
		"opacity keys": func(c *Clip) { c.Keyframes = &ClipKeyframes{Keyframes: mediagen.Keyframes{Opacity: scaleKeys(1, 0.5)}} },
		"blur keys":    func(c *Clip) { c.Keyframes = &ClipKeyframes{Blur: scaleKeys(0, 4)} },
		"a fade in":    func(c *Clip) { c.FadeInMS = 100 },
		"a fade out":   func(c *Clip) { c.FadeOutMS = 100 },
		"a trim":       func(c *Clip) { c.TrimInMS = 100 },
		"media":        func(c *Clip) { c.AssetID = "m-1" },
		"blur":         func(c *Clip) { c.Blur = 3 },
		"a flip":       func(c *Clip) { c.FlipX = true },
		"half opacity": func(c *Clip) { c.Transform.Opacity = 0.5 },
		"a layer": func(c *Clip) {
			text := Layer{ID: "t", Type: LayerText, Text: "x", FontID: "inter", FontSize: 0.05, Fill: "#ffffff", Transform: box()}
			c.Layer = &text
		},
		"a color grade":     func(c *Clip) { c.Color = &ColorGrade{} },
		"an effect stacked": func(c *Clip) { c.Vfx = []Vfx{{ID: "g", Kind: "grain"}} },
	}
	for name, change := range cases {
		d := videoDoc()
		c := nullClip()
		change(&c)
		withNullTrack(&d, c)
		if code := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks]; code != CodeInvalid {
			t.Errorf("a null with %s: got %q", name, code)
		}
	}
}

func TestNullClipsStayOffSoundTracks(t *testing.T) {
	doc := videoDoc()
	c := nullClip()
	c.StartMS = 2_000
	c.DurationMS = 1_000
	doc.Tracks[3].Clips = append(doc.Tracks[3].Clips, c)
	if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, doc)))[FieldTracks] == "" {
		t.Fatal("a null on a sound track was accepted")
	}
}
