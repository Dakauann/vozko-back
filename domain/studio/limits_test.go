package studio

import (
	"strconv"
	"testing"

	"vozko/domain/mediagen"
)

func longLane(n int, value float64) []mediagen.Keyframe {
	frames := make([]mediagen.Keyframe, n)
	for i := range frames {
		frames[i] = mediagen.Keyframe{AtMS: int64(i * 5), Value: value, Easing: mediagen.EaseLinear}
	}
	return frames
}

func TestAPropertyTakesAnyNumberOfKeys(t *testing.T) {
	doc := videoDoc()
	doc.Tracks[1].Clips[0].Keyframes = &ClipKeyframes{Keyframes: mediagen.Keyframes{X: longLane(400, 0.5)}, Blur: longLane(400, 2)}
	doc.Tracks[2].Clips[0].Keyframes = &ClipKeyframes{GainDB: longLane(1000, -6), Pan: longLane(1000, 0.2)}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatal(err)
	}
}

func TestAPathTakesAnyNumberOfKeys(t *testing.T) {
	path := make([]PositionKeyframe, 400)
	for i := range path {
		path[i] = PositionKeyframe{AtMS: int64(i * 5), X: 0.5, Y: 0.5, Easing: mediagen.EaseLinear}
	}
	doc := videoDoc()
	doc.Tracks[1].Clips[0].Keyframes = &ClipKeyframes{Position: path}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatal(err)
	}
}

func TestAVideoTakesAnyNumberOfMarkers(t *testing.T) {
	doc := videoDoc()
	for i := 0; i < 500; i++ {
		doc.Markers = append(doc.Markers, Marker{ID: "m" + strconv.Itoa(i), AtMS: int64(i)})
	}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatal(err)
	}
}

func TestAStudioVideoRunsTenMinutesWhileTheAdRenderStaysShort(t *testing.T) {
	if MaxVideoMS != 600_000 || mediagen.MaxVideoMS != 90_000 {
		t.Fatalf("studio %d, render %d", MaxVideoMS, mediagen.MaxVideoMS)
	}
	doc := videoDoc()
	doc.DurationMS = MaxVideoMS
	doc.Tracks[1].Clips[0].StartMS = MaxVideoMS - 2_000
	doc.Tracks[1].Clips[0].Keyframes = &ClipKeyframes{
		Keyframes: mediagen.Keyframes{X: []mediagen.Keyframe{{AtMS: 0, Value: 0.2, Easing: mediagen.EaseLinear}, {AtMS: 500_000, Value: 0.8, Easing: mediagen.EaseLinear}}},
		Blur:      []mediagen.Keyframe{{AtMS: 500_000, Value: 3, Easing: mediagen.EaseLinear}},
	}
	doc.Markers = []Marker{{ID: "late", AtMS: MaxVideoMS}}
	if err := ValidateDocument(KindVideo, mustJSON(t, doc)); err != nil {
		t.Fatal(err)
	}
	doc.DurationMS = MaxVideoMS + 1
	if codeOf(t, ValidateDocument(KindVideo, mustJSON(t, doc)))[FieldTracks] != CodeOutOfRange {
		t.Fatal("a video past ten minutes was accepted")
	}
}

func TestAProjectDocumentHoldsTenMegabytes(t *testing.T) {
	if MaxDocumentBytes != 10<<20 {
		t.Fatalf("got %d", MaxDocumentBytes)
	}
}

func TestAnExportHoldsTenMinutesAtTheBrowserBitrates(t *testing.T) {
	if MaxExportBytes != 512<<20 {
		t.Fatalf("got %d", MaxExportBytes)
	}
}
