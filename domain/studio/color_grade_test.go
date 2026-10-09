package studio

import "testing"

func TestColorGradeOnMediaClipsAndImageLayers(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[0].Clips[1].Color = &ColorGrade{Exposure: 0.5, Saturation: 20, Temperature: -30}
	doc.Tracks[0].Clips[0].Color = &ColorGrade{Contrast: 15}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatalf("graded media refused: %v", err)
	}
	cases := map[string]func(*VideoDocument){
		"a graded text":         func(d *VideoDocument) { d.Tracks[1].Clips[0].Color = &ColorGrade{Exposure: 1} },
		"a graded song":         func(d *VideoDocument) { d.Tracks[2].Clips[0].Color = &ColorGrade{Exposure: 1} },
		"exposure past 5 stops": func(d *VideoDocument) { d.Tracks[0].Clips[1].Color = &ColorGrade{Exposure: 6} },
		"saturation past 100":   func(d *VideoDocument) { d.Tracks[0].Clips[1].Color = &ColorGrade{Saturation: 140} },
	}
	for name, change := range cases {
		d := videoDoc()
		change(&d)
		if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, d)))[FieldTracks] == "" {
			t.Errorf("%s accepted", name)
		}
	}
	text := Layer{ID: "t", Type: LayerText, Text: "Oi", FontID: "inter", FontSize: 0.05, Fill: "#000000", Transform: box(), Color: &ColorGrade{Tint: 10}}
	if text.issue() == "" {
		t.Error("a graded text layer accepted")
	}
	photo := Layer{ID: "p", Type: LayerImage, AssetID: "img", Transform: box(), Color: &ColorGrade{Tint: 10}}
	if code := photo.issue(); code != "" {
		t.Errorf("a graded image layer refused: %s", code)
	}
}
