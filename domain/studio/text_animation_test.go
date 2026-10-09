package studio

import (
	"testing"

	"vozko/domain/mediagen"
)

func riseByWord() *TextAnimator {
	return &TextAnimator{Unit: TextUnitWord, Style: TextStyleRise, DurationMS: 400, StaggerMS: 90, Easing: "cubic-bezier(0.05,0.7,0.1,1)"}
}

func TestTextAnimatorsPlayOnTextOverlays(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[1].Clips[0].TextIn = riseByWord()
	doc.Tracks[1].Clips[0].TextOut = &TextAnimator{Unit: TextUnitChar, Style: TextStyleTypewriter, DurationMS: 55, StaggerMS: 0, Easing: mediagen.EaseLinear}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("text animation refused: %v", err)
	}
	curved := videoDoc()
	layer := *curved.Tracks[1].Clips[0].Layer
	layer.Curve = 0.3
	curved.Tracks[1].Clips[0].Layer = &layer
	curved.Tracks[1].Clips[0].TextIn = riseByWord()
	if err := ValidateDocument(KindVideo, mustJSON(t, curved)); err != nil {
		t.Fatalf("a curved text animating as one piece refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"a video clip":      func(d *VideoDocument) { d.Tracks[0].Clips[0].TextIn = riseByWord() },
		"an unknown style":  func(d *VideoDocument) { a := riseByWord(); a.Style = "spin"; d.Tracks[1].Clips[0].TextIn = a },
		"an unknown unit":   func(d *VideoDocument) { a := riseByWord(); a.Unit = "page"; d.Tracks[1].Clips[0].TextOut = a },
		"an unknown easing": func(d *VideoDocument) { a := riseByWord(); a.Easing = "wiggle"; d.Tracks[1].Clips[0].TextIn = a },
		"a long stagger":    func(d *VideoDocument) { a := riseByWord(); a.StaggerMS = 5_000; d.Tracks[1].Clips[0].TextIn = a },
		"a negative length": func(d *VideoDocument) { a := riseByWord(); a.DurationMS = -1; d.Tracks[1].Clips[0].TextIn = a },
	}
	for name, change := range cases {
		d := videoDoc()
		change(&d)
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks] == "" {
			t.Errorf("%s accepted", name)
		}
	}
}
