package studio

import (
	"strings"
	"testing"

	"vozko/domain/mediagen"
)

func TestLinksMarkersAndMotionsAreKeptInTheDocument(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[0].Clips[1].LinkID = "pair-1"
	doc.Tracks[2].Clips[0].LinkID = "pair-1"
	doc.Tracks[1].Clips[0].MotionIn = &mediagen.Motion{Edge: mediagen.EdgeBottom, DurationMS: 400}
	doc.Tracks[1].Clips[0].MotionOut = &mediagen.Motion{Edge: mediagen.EdgeTop, DurationMS: 300}
	doc.Markers = []Marker{{ID: "m1", AtMS: 1_500, Label: "Batida"}, {ID: "m2", AtMS: 0}}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatal(err)
	}
}

func TestLinksMarkersAndMotionsKeepTheirRules(t *testing.T) {
	cases := map[string]struct {
		change func(*VideoDocument)
		field  string
		code   string
	}{
		"bad link": {func(d *VideoDocument) { d.Tracks[0].Clips[0].LinkID = "Pair 1" }, FieldTracks, CodeInvalid},
		"motion on audio": {func(d *VideoDocument) {
			d.Tracks[2].Clips[0].MotionIn = &mediagen.Motion{Edge: mediagen.EdgeLeft, DurationMS: 500}
		}, FieldTracks, CodeInvalid},
		"unknown edge": {func(d *VideoDocument) {
			d.Tracks[1].Clips[0].MotionIn = &mediagen.Motion{Edge: "spin", DurationMS: 500}
		}, FieldTracks, CodeUnknown},
		"motion too long": {func(d *VideoDocument) {
			d.Tracks[1].Clips[0].MotionOut = &mediagen.Motion{Edge: mediagen.EdgeTop, DurationMS: 2_500}
		}, FieldTracks, CodeOutOfRange},
		"marker past limit": {func(d *VideoDocument) { d.Markers = []Marker{{ID: "m1", AtMS: mediagen.MaxVideoMS + 1}} }, FieldMarkers, CodeOutOfRange},
		"duplicate marker":  {func(d *VideoDocument) { d.Markers = []Marker{{ID: "m1"}, {ID: "m1", AtMS: 10}} }, FieldMarkers, CodeDuplicate},
		"bad marker id":     {func(d *VideoDocument) { d.Markers = []Marker{{ID: "M 1"}} }, FieldMarkers, CodeInvalid},
		"long marker label": {func(d *VideoDocument) {
			d.Markers = []Marker{{ID: "m1", Label: strings.Repeat("a", MaxMarkerLabelRunes+1)}}
		}, FieldMarkers, CodeTooLarge},
		"too many markers": {func(d *VideoDocument) {
			for i := 0; i <= MaxMarkers; i++ {
				d.Markers = append(d.Markers, Marker{ID: "m" + strings.Repeat("x", i%10) + string(rune('a'+i%26)) + string(rune('a'+i/26)), AtMS: int64(i)})
			}
		}, FieldMarkers, CodeTooMany},
	}
	for name, c := range cases {
		doc := videoDoc()
		c.change(&doc)
		if got := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, doc)))[c.field]; got != c.code {
			t.Errorf("%s: code %q", name, got)
		}
	}
}

func TestMotionsReachTheRenderTimeline(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[0].Clips[0].MotionIn = &mediagen.Motion{Edge: mediagen.EdgeRight, DurationMS: 600}
	doc.Tracks[1].Clips[0].MotionOut = &mediagen.Motion{Edge: mediagen.EdgeTop, DurationMS: 300}
	doc.Tracks[0].Clips[1].LinkID = "pair-1"
	doc.Markers = []Marker{{ID: "m1", AtMS: 100}}
	p, err := NewProject("ws", "u-1", KindVideo, "Reels", mustJSON(t, doc))
	if err != nil {
		t.Fatal(err)
	}
	req, err := p.VideoRequest(map[string]string{"o1": "raster-1"})
	if err != nil {
		t.Fatal(err)
	}
	if in := req.Video.Visual[0].Clips[0].MotionIn; in == nil || in.Edge != mediagen.EdgeRight || in.DurationMS != 600 {
		t.Fatalf("motion in %+v", in)
	}
	if out := req.Video.Visual[1].Clips[0].MotionOut; out == nil || out.Edge != mediagen.EdgeTop {
		t.Fatalf("motion out %+v", out)
	}
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
}
