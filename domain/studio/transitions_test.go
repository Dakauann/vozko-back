package studio

import (
	"testing"

	"vozko/domain/mediagen"
)

func cutDoc() VideoDocument {
	full := mediagen.FullFrame()
	return VideoDocument{
		Schema: SchemaVideo, Version: DocumentVersion, DurationMS: 6_000,
		Canvas: VideoCanvas{Aspect: mediagen.AspectLandscape, Background: "#000000"},
		Tracks: []Track{
			{ID: "v1", Kind: TrackVisual, Clips: []Clip{
				{ID: "a", Type: ClipVideo, AssetID: "m-a", DurationMS: 2_000, Volume: 1, Fit: mediagen.FitCover, Transform: full},
				{ID: "b", Type: ClipVideo, AssetID: "m-b", StartMS: 2_000, DurationMS: 2_000, Volume: 1, Fit: mediagen.FitCover, Transform: full},
				{ID: "c", Type: ClipVideo, AssetID: "m-c", StartMS: 5_000, DurationMS: 1_000, Volume: 1, Fit: mediagen.FitCover, Transform: full},
			}},
			{ID: "au", Kind: TrackAudio, Clips: []Clip{{ID: "m", Type: ClipAudio, AssetID: "song", DurationMS: 6_000, Volume: 1}}},
		},
	}
}

func dissolve(id string) Transition {
	return Transition{ID: id, FromClipID: "a", ToClipID: "b", Kind: TransitionDissolve, DurationMS: 500, Alignment: AlignCenter, Easing: mediagen.EaseLinear}
}

func ptr[T any](v T) *T { return &v }

func TestTransitionsSitOnTheCutBetweenTouchingClips(t *testing.T) {
	doc := cutDoc()
	push := Transition{ID: "t2", FromClipID: "a", ToClipID: "b", Kind: TransitionPush, DurationMS: 800, Alignment: AlignStart, Easing: "cubic-bezier(0.2,0,0,1)", TransitionOptions: TransitionOptions{Direction: ptr("up")}}
	doc.Tracks[0].Transitions = []Transition{push}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("push refused: %v", err)
	}
	wipe := dissolve("t3")
	wipe.Kind, wipe.Angle = TransitionWipe, ptr(0.0)
	doc.Tracks[0].Transitions = []Transition{wipe}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a wipe at angle 0 refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"clips that do not touch": func(d *VideoDocument) {
			tr := dissolve("x")
			tr.FromClipID, tr.ToClipID = "b", "c"
			d.Tracks[0].Transitions = []Transition{tr}
		},
		"a transition on sound": func(d *VideoDocument) {
			tr := dissolve("x")
			tr.FromClipID, tr.ToClipID = "m", "m"
			d.Tracks[1].Transitions = []Transition{tr}
		},
		"an unknown kind": func(d *VideoDocument) {
			tr := dissolve("x")
			tr.Kind = "spin"
			d.Tracks[0].Transitions = []Transition{tr}
		},
		"a hold easing": func(d *VideoDocument) {
			tr := dissolve("x")
			tr.Easing = mediagen.EaseHold
			d.Tracks[0].Transitions = []Transition{tr}
		},
		"an option the kind lacks": func(d *VideoDocument) {
			tr := dissolve("x")
			tr.Color = ptr("#000000")
			d.Tracks[0].Transitions = []Transition{tr}
		},
		"a length past half a clip": func(d *VideoDocument) {
			tr := dissolve("x")
			tr.DurationMS = 4_000
			d.Tracks[0].Transitions = []Transition{tr}
		},
		"a sequential kind off center": func(d *VideoDocument) {
			tr := dissolve("x")
			tr.Kind, tr.Alignment = TransitionZoom, AlignStart
			d.Tracks[0].Transitions = []Transition{tr}
		},
		"an id used by a clip": func(d *VideoDocument) { d.Tracks[0].Transitions = []Transition{dissolve("c")} },
		"two transitions on one cut": func(d *VideoDocument) {
			d.Tracks[0].Transitions = []Transition{dissolve("x"), dissolve("y")}
		},
		"an angle out of range": func(d *VideoDocument) {
			tr := dissolve("x")
			tr.Kind, tr.Angle = TransitionWipe, ptr(400.0)
			d.Tracks[0].Transitions = []Transition{tr}
		},
	}
	for name, change := range cases {
		d := cutDoc()
		change(&d)
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks] == "" {
			t.Errorf("%s accepted", name)
		}
	}
}
