package studio

import (
	"strings"
	"testing"

	"vozko/domain/mediagen"
)

func clipRepeatVideo(t *testing.T) VideoDocument {
	var doc VideoDocument
	readFixture(t, "editor_clip_repeat.json", &doc)
	return doc
}

func trailOf(d *VideoDocument) *ClipRepeat {
	return d.Tracks[0].Clips[0].Repeat
}

func ringOf(d *VideoDocument) *ClipRepeat {
	return d.Tracks[0].Clips[1].Repeat
}

func amount(v float64) *float64 { return &v }

func TestTheEditorClipRepeatDocumentPassesTheContract(t *testing.T) {
	if err := ValidateDocument(KindVideo, readFixture(t, "editor_clip_repeat.json", nil)); err != nil {
		t.Fatalf("the editor saved a clip repeat the backend refuses: %v", err)
	}
}

func TestClipRepeatsAreChecked(t *testing.T) {
	cases := map[string]func(*VideoDocument){
		"a repeat on an image clip": func(d *VideoDocument) {
			d.Tracks = append(d.Tracks, Track{ID: "pics", Kind: TrackVisual, Clips: []Clip{
				{ID: "p1", Type: ClipImage, AssetID: "m-1", DurationMS: 1_000, Fit: mediagen.FitCover, Transform: mediagen.FullFrame(), Repeat: &ClipRepeat{Mode: RepeatLinear, Copies: 2}},
			}})
		},
		"a repeat on a song": func(d *VideoDocument) {
			d.Tracks = append(d.Tracks, Track{ID: "song", Kind: TrackAudio, Clips: []Clip{
				{ID: "s1", Type: ClipAudio, AssetID: "song", DurationMS: 1_000, Volume: 1, Repeat: &ClipRepeat{Mode: RepeatLinear, Copies: 2}},
			}})
		},
		"a linear repeat with rows":      func(d *VideoDocument) { trailOf(d).Rows = 3 },
		"a linear repeat without copies": func(d *VideoDocument) { trailOf(d).Copies = 0 },
		"a scale of zero":                func(d *VideoDocument) { trailOf(d).Scale = amount(0) },
		"an end opacity past one":        func(d *VideoDocument) { trailOf(d).EndOpacity = amount(2) },
		"a shift out of range":           func(d *VideoDocument) { trailOf(d).Shift = 20_000 },
		"an unknown mode":                func(d *VideoDocument) { trailOf(d).Mode = "spiral" },
		"an unknown order":               func(d *VideoDocument) { trailOf(d).Order = "middle" },
		"a grid repeat with x": func(d *VideoDocument) {
			*ringOf(d) = ClipRepeat{Mode: RepeatGrid, Repeat: Repeat{Rows: 2, Columns: 2}, X: 4}
		},
		"a radial repeat without a count": func(d *VideoDocument) { ringOf(d).Count = 0 },
		"a key on x in radial mode": func(d *VideoDocument) {
			ringOf(d).Keyframes.X = []mediagen.Keyframe{{Value: 4, Easing: mediagen.EaseLinear}}
		},
		"a key out of range": func(d *VideoDocument) {
			trailOf(d).Keyframes.Copies[0].Value = 0
		},
	}
	for name, change := range cases {
		d := clipRepeatVideo(t)
		change(&d)
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks] == "" {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestClipRepeatCopiesHaveAFloorButNoCeiling(t *testing.T) {
	d := clipRepeatVideo(t)
	trailOf(&d).Copies = 900
	trailOf(&d).Keyframes.Copies[1].Value = 5_000
	if err := ValidateDocument(KindVideo, mustJSON(t, d)); err != nil {
		t.Fatalf("many copies were refused: %v", err)
	}
}

func TestEveryClipRepeatModeIsAccepted(t *testing.T) {
	for name, repeat := range map[string]ClipRepeat{
		"grid":   {Mode: RepeatGrid, Repeat: Repeat{Rows: 2, Columns: 3, GapX: 10}, Keyframes: &ClipRepeatKeyframes{GapX: []mediagen.Keyframe{{Value: 20, Easing: mediagen.EaseLinear}}}},
		"mirror": {Mode: RepeatMirror, Repeat: Repeat{Angle: 90, Gap: 4}, StartOpacity: amount(0.5)},
	} {
		d := clipRepeatVideo(t)
		*ringOf(&d) = repeat
		if err := ValidateDocument(KindVideo, mustJSON(t, d)); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
	}
}

func TestClipRepeatFieldsAreStrict(t *testing.T) {
	raw := strings.Replace(string(readFixture(t, "editor_clip_repeat.json", nil)), `"mode": "linear",`, `"mode": "linear", "speed": 2,`, 1)
	if codeOf(t, ValidateDocument(KindVideo, []byte(raw)))[FieldDocument] == "" {
		t.Fatal("an unknown repeat field was accepted")
	}
	whole := strings.Replace(string(readFixture(t, "editor_clip_repeat.json", nil)), `"copies": 5,`, `"copies": 2.5,`, 1)
	if codeOf(t, ValidateDocument(KindVideo, []byte(whole)))[FieldDocument] == "" {
		t.Fatal("fractional copies were accepted")
	}
}
