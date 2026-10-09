package studio

import (
	"encoding/json"
	"testing"
)

func TestADisabledClipIsKeptInTheDocument(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[0].Clips[1].Disabled = true
	doc.Tracks[1].Clips[0].Disabled = true
	p, err := NewProject("ws", "u-1", KindVideo, "Reels", mustJSON(t, doc))
	if err != nil {
		t.Fatal(err)
	}
	var saved VideoDocument
	if err := json.Unmarshal(p.Document, &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.Tracks[0].Clips[1].Disabled || !saved.Tracks[1].Clips[0].Disabled {
		t.Fatalf("tracks %+v", saved.Tracks)
	}
}
