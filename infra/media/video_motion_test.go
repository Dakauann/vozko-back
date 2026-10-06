package media_infra

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/mediagen"
)

func TestAMotionCompilesToAnEasedOverlayPosition(t *testing.T) {
	c := mediagen.Clip{StartMS: 1_000, DurationMS: 3_000, Transform: mediagen.Transform{X: 0.5, Y: 0.25},
		MotionIn: &mediagen.Motion{Edge: mediagen.EdgeLeft, DurationMS: 500}, MotionOut: &mediagen.Motion{Edge: mediagen.EdgeBottom, DurationMS: 250}}
	x, y := overlayPosition(c)
	if !strings.Contains(x, "0.500000*W-overlay_w/2") || !strings.Contains(x, "-0.200000*W*pow(1-clip((t-1.000)/0.500,0,1),3)") {
		t.Fatalf("x %s", x)
	}
	if !strings.Contains(y, "0.200000*H*pow(1-clip((4.000-t)/0.250,0,1),3)") || strings.Contains(y, "(t-1.000)") {
		t.Fatalf("y %s", y)
	}
	still := mediagen.Clip{StartMS: 0, DurationMS: 1_000, Transform: mediagen.Transform{X: 0.5, Y: 0.5}}
	if x, y := overlayPosition(still); x != "0.500000*W-overlay_w/2" || y != "0.500000*H-overlay_h/2" {
		t.Fatalf("a still clip keeps a plain position: %s %s", x, y)
	}
}

func TestAnOverlaySlidesInFromItsEdge(t *testing.T) {
	requireFFmpeg(t)
	srv, sources := renderSources(t)
	timeline := editedTimeline()
	overlay := &timeline.Visual[1].Clips[0]
	overlay.Transform.Rotation = 0
	overlay.MotionIn = &mediagen.Motion{Edge: mediagen.EdgeLeft, DurationMS: 1_000}
	req := mediagen.Request{Kind: mediagen.KindVideo, WorkspaceID: "ws", Aspect: mediagen.AspectStory, Video: timeline}
	out, err := NewVideoRenderer(srv.Client()).Generate(context.Background(), req, sources)
	if err != nil {
		t.Fatal(err)
	}
	entering, resting := centerPixel(t, out.Bytes, "1.05"), centerPixel(t, out.Bytes, "1.8")
	if entering[0] < 180 || entering[2] > 80 {
		t.Fatalf("while it enters from the left the centre still shows the photo, got %v", entering)
	}
	if resting[2] < 150 || resting[0] > 100 {
		t.Fatalf("once in place the overlay covers the centre, got %v", resting)
	}
}
