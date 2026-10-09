package studio

import "testing"

func TestClipSpeedOnlyWhereMediaHasTime(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[0].Clips[1].Speed = ptr(0.25)
	doc.Tracks[0].Clips[1].Reverse = true
	doc.Tracks[0].Clips[1].PreservePitch = ptr(false)
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("a slow reversed video refused: %v", err)
	}
	frozen := videoDoc()
	frozen.Tracks[0].Clips[1].Freeze = true
	if err := ValidateDocument(KindVideo, mustJSON(t, frozen)); err != nil {
		t.Fatalf("a freeze frame refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"a speed past 16x":   func(d *VideoDocument) { d.Tracks[0].Clips[1].Speed = ptr(20.0) },
		"a zero speed":       func(d *VideoDocument) { d.Tracks[0].Clips[1].Speed = ptr(0.0) },
		"a frozen fast clip": func(d *VideoDocument) { d.Tracks[0].Clips[1].Freeze, d.Tracks[0].Clips[1].Speed = true, ptr(2.0) },
		"a frozen reversed clip": func(d *VideoDocument) {
			d.Tracks[0].Clips[1].Freeze, d.Tracks[0].Clips[1].Reverse = true, true
		},
		"a sped up text":  func(d *VideoDocument) { d.Tracks[1].Clips[0].Speed = ptr(2.0) },
		"a sped up image": func(d *VideoDocument) { d.Tracks[0].Clips[0].Speed = ptr(2.0) },
		"a frozen song":   func(d *VideoDocument) { d.Tracks[2].Clips[0].Freeze = true },
	}
	for name, change := range cases {
		d := videoDoc()
		change(&d)
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks] == "" {
			t.Errorf("%s accepted", name)
		}
	}
}
