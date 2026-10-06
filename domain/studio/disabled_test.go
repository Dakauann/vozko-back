package studio

import "testing"

func TestADisabledClipIsKeptButNotRendered(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[0].Clips[1].Disabled = true
	doc.Tracks[1].Clips[0].Disabled = true
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatal(err)
	}
	p, err := NewProject("ws", "u-1", KindVideo, "Reels", mustJSON(t, doc))
	if err != nil {
		t.Fatal(err)
	}
	req, err := p.VideoRequest(nil)
	if err != nil {
		t.Fatalf("a disabled overlay needs no raster: %v", err)
	}
	if len(req.Video.Visual) != 1 || len(req.Video.Visual[0].Clips) != 1 || req.Video.Visual[0].Clips[0].MediaID != "m-1" {
		t.Fatalf("visual %+v", req.Video.Visual)
	}
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
}
