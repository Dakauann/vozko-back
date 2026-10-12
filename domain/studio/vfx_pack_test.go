package studio

import (
	"testing"

	"vozko/domain/mediagen"
)

func TestSoundEffectsListenToAnAudioTrackOfTheirTimeline(t *testing.T) {
	accepted := map[string]func(*VideoDocument){
		"a spectrum on a picture": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Vfx = []Vfx{{ID: "s", Kind: "audioSpectrum", TrackID: "music", Params: map[string]*float64{"count": float(48), "gain": float(-6)}}}
		},
		"a waveform on an effect clip": func(d *VideoDocument) {
			withEffectTrack(d, effectClip(Vfx{ID: "w", Kind: "audioWaveform", TrackID: "muted", Params: map[string]*float64{"window": float(1_000)}}))
		},
		"a spectrum inside a group listening to the group's own sound": func(d *VideoDocument) {
			group := plainGroup()
			group.Tracks[0].Clips[0].Vfx = []Vfx{{ID: "s", Kind: "audioSpectrum", TrackID: "g1-sound"}}
			group.Tracks = append(group.Tracks, Track{ID: "g1-sound", Kind: TrackAudio})
			withGroupTrack(d, group)
		},
	}
	for name, change := range accepted {
		d := videoDoc()
		change(&d)
		if err := ValidateDocument(KindVideo, mustJSON(t, d)); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
	}
	refused := map[string]func(*VideoDocument){
		"a spectrum without a track": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Vfx = []Vfx{{ID: "s", Kind: "audioSpectrum"}}
		},
		"a spectrum on a picture track": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Vfx = []Vfx{{ID: "s", Kind: "audioSpectrum", TrackID: "text"}}
		},
		"a waveform on a missing track": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Vfx = []Vfx{{ID: "w", Kind: "audioWaveform", TrackID: "gone"}}
		},
		"a track on an effect that does not listen": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Vfx = []Vfx{{ID: "g", Kind: "glow", TrackID: "music"}}
		},
		"a spectrum inside a group listening to the outer sound": func(d *VideoDocument) {
			group := plainGroup()
			group.Tracks[0].Clips[0].Vfx = []Vfx{{ID: "s", Kind: "audioSpectrum", TrackID: "music"}}
			withGroupTrack(d, group)
		},
		"a key on the bar count": func(d *VideoDocument) {
			d.Tracks[0].Clips[0].Vfx = []Vfx{{ID: "s", Kind: "audioSpectrum", TrackID: "music", Keyframes: map[string][]mediagen.Keyframe{"count": {{AtMS: 0, Value: 8, Easing: mediagen.EaseLinear}}}}}
		},
	}
	for name, change := range refused {
		d := videoDoc()
		change(&d)
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks] == "" {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestEchoRepeatsAClipButNeverAnEffectClip(t *testing.T) {
	d := videoDoc()
	d.Tracks[1].Clips[0].Vfx = []Vfx{{ID: "e", Kind: "echo", Params: map[string]*float64{"count": float(12), "delay": float(40)}, Keyframes: map[string][]mediagen.Keyframe{"decay": {{AtMS: 0, Value: 0.2, Easing: mediagen.EaseLinear}, {AtMS: 900, Value: 0.9, Easing: mediagen.EaseOut}}}}}
	withEffectTrack(&d, effectClip(Vfx{ID: "p", Kind: "posterizeTime", Params: map[string]*float64{"rate": float(4)}}))
	if err := ValidateDocument(KindVideo, mustJSON(t, d)); err != nil {
		t.Fatalf("echo on a text and posterize time on an effect clip refused: %v", err)
	}
	withEffectTrack(&d, effectClip(Vfx{ID: "e2", Kind: "echo"}))
	d.Tracks[len(d.Tracks)-1].ID = "more-effects"
	d.Tracks[len(d.Tracks)-1].Clips[0].ID = "fx-2"
	if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks] == "" {
		t.Fatal("an echo on an effect clip was accepted")
	}
}

func TestTheNewEffectsKeepTheirRanges(t *testing.T) {
	cases := []struct {
		kind, param string
		value       float64
		want        string
	}{
		{"glow", "size", 200, ""},
		{"glow", "size", 0.5, CodeOutOfRange},
		{"twirl", "angle", -1080, ""},
		{"twirl", "angle", 1081, CodeOutOfRange},
		{"bulge", "height", -1, ""},
		{"cornerPin", "bottomLeftY", 1.5, ""},
		{"cornerPin", "bottomLeftY", 1.6, CodeOutOfRange},
		{"posterize", "levels", 1, CodeOutOfRange},
		{"echo", "count", 33, CodeOutOfRange},
		{"audioWaveform", "window", 10_000, ""},
		{"mosaic", "angle", 10, CodeInvalid},
	}
	for _, c := range cases {
		if got := VfxParamIssue(c.kind, c.param, c.value); got != c.want {
			t.Errorf("%s.%s = %g: got %q, want %q", c.kind, c.param, c.value, got, c.want)
		}
	}
}

func TestTheNewStillEffectsOnPhotosAndArtboards(t *testing.T) {
	board := artboard("feed", 0, stillPhoto(
		Vfx{ID: "g", Kind: "glow", Params: map[string]*float64{"threshold": float(0.3)}},
		Vfx{ID: "c", Kind: "cornerPin", Params: map[string]*float64{"topLeftX": float(0.1)}},
		Vfx{ID: "w", Kind: "waveWarp", Params: map[string]*float64{"phase": float(90)}},
	))
	board.Canvas.Vfx = []Vfx{{ID: "n", Kind: "fractalNoise", Seed: 9, Params: map[string]*float64{"complexity": float(2.5)}}}
	if err := ValidateDocument(KindImage, mustJSON(t, project(board))); err != nil {
		t.Fatalf("still effects refused: %v", err)
	}
	moving := map[string]Vfx{
		"a wave speed":       {ID: "w", Kind: "waveWarp", Params: map[string]*float64{"speed": float(2)}},
		"a noise evolution":  {ID: "n", Kind: "fractalNoise", Params: map[string]*float64{"speed": float(1)}},
		"an echo":            {ID: "e", Kind: "echo"},
		"a sound visualizer": {ID: "s", Kind: "audioSpectrum", TrackID: "music"},
		"posterize time":     {ID: "p", Kind: "posterizeTime"},
	}
	for name, vfx := range moving {
		if ValidateDocument(KindImage, mustJSON(t, project(artboard("feed", 0, stillPhoto(vfx))))) == nil {
			t.Errorf("%s accepted on a photo", name)
		}
	}
}
