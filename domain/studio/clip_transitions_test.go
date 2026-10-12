package studio

import (
	"encoding/json"
	"os"
	"testing"

	"vozko/domain/mediagen"
)

func TestTheEditorTransitionDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_transitions.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindVideo, raw); err != nil {
		t.Fatalf("the editor saved a transition document the backend refuses: %v", err)
	}
	var doc VideoDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	second := doc.Tracks[0].Clips[1]
	if second.TransitionOut == nil || second.TransitionOut.MapAssetID == nil || *second.TransitionOut.MapAssetID != "m-3" || second.TransitionOut.Map != nil {
		t.Fatalf("the picture map was lost: %+v", second.TransitionOut)
	}
	if cut := doc.Tracks[0].Transitions[0]; cut.Kind != TransitionVenetianBlinds || cut.Size == nil || *cut.Size != 0.1 {
		t.Fatalf("the blinds lost their size: %+v", cut)
	}
}

func edgeWipe() *ClipTransition {
	return &ClipTransition{Kind: TransitionWipe, DurationMS: 500, Easing: mediagen.EaseLinear}
}

func TestEntranceAndExitTransitionsFollowTheSideRules(t *testing.T) {
	doc := cutDoc()
	doc.Tracks[0].Clips[2].TransitionIn = &ClipTransition{Kind: TransitionRadialWipe, DurationMS: 400, Easing: mediagen.EaseOut, TransitionOptions: TransitionOptions{Softness: ptr(0.2), OriginY: ptr(1.0)}}
	doc.Tracks[0].Clips[2].TransitionOut = &ClipTransition{Kind: TransitionSlide, DurationMS: 300, Easing: mediagen.EaseLinear, TransitionOptions: TransitionOptions{Direction: ptr("left")}}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("entrance and exit refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"a plain dissolve, which is the fade": func(d *VideoDocument) {
			d.Tracks[0].Clips[2].TransitionIn = &ClipTransition{Kind: TransitionDissolve, DurationMS: 300, Easing: mediagen.EaseLinear}
		},
		"a sound crossfade on a picture": func(d *VideoDocument) {
			d.Tracks[0].Clips[2].TransitionIn = &ClipTransition{Kind: TransitionConstantPower, DurationMS: 300, Easing: mediagen.EaseLinear}
		},
		"the slide side, which the edge decides": func(d *VideoDocument) {
			d.Tracks[0].Clips[2].TransitionIn = &ClipTransition{Kind: TransitionSlide, DurationMS: 300, Easing: mediagen.EaseLinear, TransitionOptions: TransitionOptions{Moving: ptr("in")}}
		},
		"a fade on the same side": func(d *VideoDocument) {
			d.Tracks[0].Clips[2].TransitionIn = edgeWipe()
			d.Tracks[0].Clips[2].FadeInMS = 200
		},
		"a slide preset on the same side": func(d *VideoDocument) {
			d.Tracks[0].Clips[2].TransitionOut = edgeWipe()
			d.Tracks[0].Clips[2].MotionOut = &mediagen.Motion{Edge: mediagen.EdgeTop, DurationMS: 200}
		},
		"edges longer than the clip": func(d *VideoDocument) {
			d.Tracks[0].Clips[2].TransitionIn = &ClipTransition{Kind: TransitionWipe, DurationMS: 800, Easing: mediagen.EaseLinear}
			d.Tracks[0].Clips[2].FadeOutMS = 300
		},
		"a hold curve": func(d *VideoDocument) {
			d.Tracks[0].Clips[2].TransitionIn = &ClipTransition{Kind: TransitionWipe, DurationMS: 300, Easing: mediagen.EaseHold}
		},
		"a length under the minimum": func(d *VideoDocument) {
			d.Tracks[0].Clips[2].TransitionIn = &ClipTransition{Kind: TransitionWipe, DurationMS: 50, Easing: mediagen.EaseLinear}
		},
		"an entrance on a sound": func(d *VideoDocument) { d.Tracks[1].Clips[0].TransitionIn = edgeWipe() },
		"an entrance on an edge joined by a cut transition": func(d *VideoDocument) {
			d.Tracks[0].Transitions = []Transition{dissolve("x")}
			d.Tracks[0].Clips[1].TransitionIn = edgeWipe()
		},
		"an exit on an edge joined by a cut transition": func(d *VideoDocument) {
			d.Tracks[0].Transitions = []Transition{dissolve("x")}
			d.Tracks[0].Clips[0].TransitionOut = edgeWipe()
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

func TestTheNewWipesCheckTheirOptions(t *testing.T) {
	valid := map[string]Transition{
		"gradient":         {Kind: TransitionGradientWipe, TransitionOptions: TransitionOptions{Map: ptr("clouds"), Softness: ptr(1.0), Reverse: ptr(true)}},
		"gradient picture": {Kind: TransitionGradientWipe, TransitionOptions: TransitionOptions{MapAssetID: ptr("m-9")}},
		"blinds":           {Kind: TransitionVenetianBlinds, TransitionOptions: TransitionOptions{Angle: ptr(45.0), Size: ptr(0.01)}},
		"blocks":           {Kind: TransitionBlockDissolve, TransitionOptions: TransitionOptions{Size: ptr(1.0), Softness: ptr(0.0)}},
		"fan":              {Kind: TransitionRadialWipe, TransitionOptions: TransitionOptions{Angle: ptr(359.0), OriginX: ptr(0.0)}},
	}
	for name, tr := range valid {
		doc := cutDoc()
		tr.ID, tr.FromClipID, tr.ToClipID, tr.DurationMS, tr.Alignment, tr.Easing = "x", "a", "b", 600, AlignCenter, mediagen.EaseLinear
		doc.Tracks[0].Transitions = []Transition{tr}
		if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
	}
	refused := map[string]Transition{
		"a map and a picture together": {Kind: TransitionGradientWipe, TransitionOptions: TransitionOptions{Map: ptr("linear"), MapAssetID: ptr("m-9")}},
		"an unknown map":               {Kind: TransitionGradientWipe, TransitionOptions: TransitionOptions{Map: ptr("spiral")}},
		"a blank picture":              {Kind: TransitionGradientWipe, TransitionOptions: TransitionOptions{MapAssetID: ptr("  ")}},
		"softness past one":            {Kind: TransitionRadialWipe, TransitionOptions: TransitionOptions{Softness: ptr(1.5)}},
		"blinds with no width":         {Kind: TransitionVenetianBlinds, TransitionOptions: TransitionOptions{Size: ptr(0.0)}},
		"blocks wider than the frame":  {Kind: TransitionBlockDissolve, TransitionOptions: TransitionOptions{Size: ptr(1.5)}},
		"a direction on blinds":        {Kind: TransitionVenetianBlinds, TransitionOptions: TransitionOptions{Direction: ptr("left")}},
		"a size on a plain wipe":       {Kind: TransitionWipe, TransitionOptions: TransitionOptions{Size: ptr(0.1)}},
	}
	for name, tr := range refused {
		doc := cutDoc()
		tr.ID, tr.FromClipID, tr.ToClipID, tr.DurationMS, tr.Alignment, tr.Easing = "x", "a", "b", 600, AlignCenter, mediagen.EaseLinear
		doc.Tracks[0].Transitions = []Transition{tr}
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, doc)))[FieldTracks] == "" {
			t.Errorf("%s accepted", name)
		}
	}
}
