package studio

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"vozko/domain/mediagen"
)

func editorGroupDoc(t *testing.T) VideoDocument {
	t.Helper()
	raw, err := os.ReadFile("testdata/editor_group.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc VideoDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func introOf(d *VideoDocument) *Clip { return &d.Tracks[1].Clips[0] }

func plainGroup() Clip {
	full := mediagen.FullFrame()
	return Clip{
		ID: "g1", Type: ClipGroup, Name: "Cena", StartMS: 0, DurationMS: 2_000, Volume: 1, Transform: full,
		Tracks: []Track{{ID: "g1-pictures", Kind: TrackVisual, Clips: []Clip{
			{ID: "g1-shot", Type: ClipImage, AssetID: "m-9", DurationMS: 2_000, Fit: mediagen.FitCover, Transform: full},
		}}},
	}
}

func withGroupTrack(d *VideoDocument, group Clip) {
	d.Tracks = append(d.Tracks, Track{ID: "groups", Kind: TrackVisual, Clips: []Clip{group}})
}

func TestTheEditorGroupDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_group.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindVideo, raw); err != nil {
		t.Fatalf("the editor saved a grouped document the backend refuses: %v", err)
	}
	doc := editorGroupDoc(t)
	intro := introOf(&doc)
	if doc.Tracks[1].ID != "titles" || intro.ID != "intro" || intro.Type != ClipGroup || intro.Name != "Abertura" {
		t.Fatalf("group identity lost: %q %q %q %q", doc.Tracks[1].ID, intro.ID, intro.Type, intro.Name)
	}
	if intro.Speed == nil || *intro.Speed != 1.25 || intro.ParentID != "rig" || len(intro.Tracks) != 6 {
		t.Fatalf("group fields lost: speed %v parent %q tracks %d", intro.Speed, intro.ParentID, len(intro.Tracks))
	}
	titles := intro.Tracks[2]
	if titles.ID != "inner-titles" || len(titles.Clips) != 2 || titles.Clips[1].ID != "logo" || titles.Clips[1].Type != ClipGroup || len(titles.Clips[1].Tracks) != 1 {
		t.Fatalf("nested group lost: %+v", titles)
	}
	pictures := intro.Tracks[0]
	if len(pictures.Transitions) != 1 || pictures.Transitions[0].ID != "inner-cut" {
		t.Fatalf("inner transition lost: %+v", pictures.Transitions)
	}
	music := intro.Tracks[5]
	if music.ID != "inner-music" || music.Ducking == nil || len(music.Ducking.TriggerTrackIDs) != 1 || music.Ducking.TriggerTrackIDs[0] != "inner-voice" {
		t.Fatalf("inner ducking lost: %+v", music.Ducking)
	}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("the grouped document stopped passing after a round trip: %v", err)
	}
}

func TestGroupedDocumentsRefuseWhatAGroupCannotCarry(t *testing.T) {
	no := false
	cases := map[string]func(*VideoDocument){
		"a reversed group":      func(d *VideoDocument) { introOf(d).Reverse = true },
		"a group keeping pitch": func(d *VideoDocument) { introOf(d).PreservePitch = &no },
		"a frozen group":        func(d *VideoDocument) { introOf(d).Freeze = true },
		"a group with an asset": func(d *VideoDocument) { introOf(d).AssetID = "m-backdrop" },
		"a group with a fit":    func(d *VideoDocument) { introOf(d).Fit = mediagen.FitCover },
		"a group with a chroma key": func(d *VideoDocument) {
			introOf(d).ChromaKey = &ChromaKey{Color: "#00ff00", Similarity: 0.4, Smoothness: 0.1}
		},
		"a group with a luma key":     func(d *VideoDocument) { introOf(d).LumaKey = &LumaKey{Mode: LumaDark, Threshold: 0.2, Softness: 0.1} },
		"a group with a gradient map": func(d *VideoDocument) { introOf(d).GradientMap = &GradientMap{} },
		"a group with behaviors":      func(d *VideoDocument) { introOf(d).Behaviors = []Behavior{{ID: "b1"}} },
		"a group with a blank name":   func(d *VideoDocument) { introOf(d).Name = "   " },
		"a group with a speed lane": func(d *VideoDocument) {
			introOf(d).Keyframes.Speed = []mediagen.Keyframe{{AtMS: 0, Value: 1, Easing: mediagen.EaseLinear}}
		},
		"an inner id repeating a root": func(d *VideoDocument) { introOf(d).Tracks[0].Clips[0].ID = "backdrop" },
		"an inner track id repeating":  func(d *VideoDocument) { introOf(d).Tracks[0].ID = "base" },
		"an inner parent outside":      func(d *VideoDocument) { introOf(d).Tracks[2].Clips[0].ParentID = "rig" },
		"an inner trigger outside":     func(d *VideoDocument) { introOf(d).Tracks[5].Ducking.TriggerTrackIDs = []string{"base"} },
		"an inner clip overlapping":    func(d *VideoDocument) { introOf(d).Tracks[0].Clips[1].StartMS = 1_000 },
		"an inner clip of no kind":     func(d *VideoDocument) { introOf(d).Tracks[0].Clips[0].Type = "sticker" },
		"an inner track of no kind":    func(d *VideoDocument) { introOf(d).Tracks[0].Kind = "subtitle" },
		"an inner clip past the bound": func(d *VideoDocument) { introOf(d).Tracks[1].Clips[0].DurationMS = MaxVideoMS + 1 },
		"a non group with tracks": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Tracks = []Track{{ID: "loose", Kind: TrackVisual, Clips: []Clip{}}}
		},
		"a blank clip name":             func(d *VideoDocument) { d.Tracks[0].Clips[0].Name = "   " },
		"a nested group asset":          func(d *VideoDocument) { introOf(d).Tracks[2].Clips[1].AssetID = "m-logo" },
		"a nested inner id repeating":   func(d *VideoDocument) { introOf(d).Tracks[2].Clips[1].Tracks[0].Clips[0].ID = "shot-a" },
		"an inner transition id repeat": func(d *VideoDocument) { introOf(d).Tracks[0].Transitions[0].ID = "intro" },
		"a group on a sound track": func(d *VideoDocument) {
			group := plainGroup()
			d.Tracks = append(d.Tracks, Track{ID: "sounds", Kind: TrackAudio, Clips: []Clip{group}})
		},
	}
	for name, change := range cases {
		d := editorGroupDoc(t)
		change(&d)
		if err := ValidateDocument(KindVideo, mustJSON(t, d)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAGroupRoundTripsItsNameAndTimeline(t *testing.T) {
	group := plainGroup()
	raw := mustJSON(t, group)
	var back Clip
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Type != ClipGroup || back.Name != "Cena" || len(back.Tracks) != 1 || len(back.Tracks[0].Clips) != 1 || back.Tracks[0].Clips[0].AssetID != "m-9" {
		t.Fatalf("group lost on a round trip: %+v", back)
	}
	doc := videoDoc()
	withGroupTrack(&doc, group)
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a plain group was refused: %v", err)
	}
}

func TestAGroupKeepsItsSpeedAndNameWithinRange(t *testing.T) {
	fast := 20.0
	cases := map[string]struct {
		change func(*Clip)
		code   string
	}{
		"a speed past the range": {func(c *Clip) { c.Speed = &fast }, CodeOutOfRange},
		"a name too long":        {func(c *Clip) { c.Name = strings.Repeat("a", maxTokenRunes*2+1) }, CodeTooLarge},
		"a blank name":           {func(c *Clip) { c.Name = " " }, CodeInvalid},
	}
	for name, tc := range cases {
		group := plainGroup()
		tc.change(&group)
		doc := videoDoc()
		withGroupTrack(&doc, group)
		if code := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, doc)))[FieldTracks]; code != tc.code {
			t.Errorf("%s: got %q want %q", name, code, tc.code)
		}
	}
	slow := 0.1
	group := plainGroup()
	group.Speed = &slow
	group.Name = strings.Repeat("a", maxTokenRunes*2)
	doc := videoDoc()
	withGroupTrack(&doc, group)
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a slow group with a full name was refused: %v", err)
	}
}
