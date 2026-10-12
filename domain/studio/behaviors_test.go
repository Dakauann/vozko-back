package studio

import (
	"encoding/json"
	"os"
	"testing"
)

func number(v float64) *float64 { return &v }

func whole(v int64) *int64 { return &v }

func wiggleOn(property string) Behavior {
	return Behavior{ID: "w", Kind: "wiggle", Property: property, Frequency: number(2), Amount: number(0.05), Seed: whole(9)}
}

func reactTo(trackID string) Behavior {
	return Behavior{ID: "r", Kind: "audioReact", Property: "scale", TrackID: trackID, Amount: number(0.2), FloorDB: number(-40), CeilingDB: number(-10)}
}

func TestTheEditorBehaviorDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_behaviors.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindVideo, raw); err != nil {
		t.Fatalf("the editor saved behaviors the backend refuses: %v", err)
	}
	var doc VideoDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	photo, rig := doc.Tracks[0].Clips[0], doc.Tracks[1].Clips[0]
	if len(photo.Behaviors) != 5 || len(rig.Behaviors) != 2 {
		t.Fatalf("behaviors lost: %d on the photo, %d on the null", len(photo.Behaviors), len(rig.Behaviors))
	}
	float := rig.Behaviors[0]
	if float.Amount == nil || *float.Amount != 0 || float.Seed == nil || *float.Seed != 0 || float.OffsetMS == nil || *float.OffsetMS != 1200 {
		t.Fatalf("zero values must survive as present: %+v", float)
	}
	if pulse := photo.Behaviors[4]; pulse.TrackID != "music" || *pulse.Amount != -0.4 {
		t.Fatalf("the sound reaction lost its fields: %+v", pulse)
	}
}

func TestBehaviorsAreCheckedLikeTheEditor(t *testing.T) {
	ok := videoDoc()
	ok.Tracks[0].Clips[0].Behaviors = []Behavior{wiggleOn("x"), {ID: "l", Kind: "loop", Property: "rotation", Mode: "cycle", Side: "out"}, reactTo("music")}
	if err := ValidateDocument(KindVideo, mustJSON(t, ok)); err != nil {
		t.Fatalf("valid behaviors were refused: %v", err)
	}
	cases := map[string]struct {
		behaviors []Behavior
		code      string
	}{
		"a slow wiggle":           {[]Behavior{{ID: "w", Kind: "wiggle", Property: "x", Frequency: number(0), Amount: number(0.1), Seed: whole(1)}}, CodeOutOfRange},
		"a wiggle past the span":  {[]Behavior{{ID: "w", Kind: "wiggle", Property: "opacity", Frequency: number(2), Amount: number(1.5), Seed: whole(1)}}, CodeOutOfRange},
		"a seed past the range":   {[]Behavior{{ID: "w", Kind: "wiggle", Property: "x", Frequency: number(2), Amount: number(0.1), Seed: whole(1_000_000)}}, CodeOutOfRange},
		"an unknown kind":         {[]Behavior{{ID: "w", Kind: "spin", Property: "x", Rate: number(1)}}, CodeUnknown},
		"a sound property":        {[]Behavior{{ID: "w", Kind: "drift", Property: "gainDb", Rate: number(1)}}, CodeUnknown},
		"a field of another kind": {[]Behavior{{ID: "w", Kind: "drift", Property: "x", Rate: number(1), Seed: whole(2)}}, CodeInvalid},
		"a missing field":         {[]Behavior{{ID: "w", Kind: "overshoot", Property: "scale", Amplitude: number(0.1), Frequency: number(3)}}, CodeInvalid},
		"an unknown loop":         {[]Behavior{{ID: "l", Kind: "loop", Property: "x", Mode: "bounce", Side: "out"}}, CodeUnknown},
		"a drift past the span":   {[]Behavior{{ID: "d", Kind: "drift", Property: "rotation", Rate: number(8000)}}, CodeOutOfRange},
		"a strong overshoot":      {[]Behavior{{ID: "o", Kind: "overshoot", Property: "scale", Amplitude: number(2), Frequency: number(3), Decay: number(5)}}, CodeOutOfRange},
		"a ceiling under floor":   {[]Behavior{{ID: "r", Kind: "audioReact", Property: "scale", TrackID: "music", Amount: number(0.1), FloorDB: number(-10), CeilingDB: number(-20)}}, CodeOutOfRange},
		"a repeated kind":         {[]Behavior{wiggleOn("x"), {ID: "w2", Kind: "wiggle", Property: "x", Frequency: number(1), Amount: number(0.1), Seed: whole(3)}}, CodeDuplicate},
		"a repeated id":           {[]Behavior{wiggleOn("x"), {ID: "w", Kind: "drift", Property: "x", Rate: number(1)}}, CodeDuplicate},
		"a negative offset":       {[]Behavior{{ID: "w", Kind: "drift", Property: "x", Rate: number(1), OffsetMS: whole(-1)}}, CodeOutOfRange},
		"a visual track reaction": {[]Behavior{reactTo("main")}, CodeInvalid},
		"a lost track reaction":   {[]Behavior{reactTo("ghost")}, CodeInvalid},
	}
	for name, tc := range cases {
		d := videoDoc()
		d.Tracks[0].Clips[0].Behaviors = tc.behaviors
		if code := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks]; code != tc.code {
			t.Errorf("%s: got %q, want %q", name, code, tc.code)
		}
	}
}

func TestBehaviorsStayOnClipsThatMove(t *testing.T) {
	rig := nullClip()
	rig.Behaviors = []Behavior{wiggleOn("y")}
	d := videoDoc()
	withNullTrack(&d, rig)
	if err := ValidateDocument(KindVideo, mustJSON(t, d)); err != nil {
		t.Fatalf("a null that wiggles was refused: %v", err)
	}
	refused := map[string]func(*VideoDocument){
		"a null fading": func(d *VideoDocument) {
			c := nullClip()
			c.Behaviors = []Behavior{wiggleOn("opacity")}
			withNullTrack(d, c)
		},
		"a sound clip": func(d *VideoDocument) { d.Tracks[2].Clips[0].Behaviors = []Behavior{wiggleOn("x")} },
		"an effect clip": func(d *VideoDocument) {
			c := effectClip()
			c.Behaviors = []Behavior{wiggleOn("x")}
			withEffectTrack(d, c)
		},
	}
	for name, change := range refused {
		d := videoDoc()
		change(&d)
		if code := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks]; code != CodeInvalid {
			t.Errorf("%s: got %q", name, code)
		}
	}
}
