package media_infra

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	media_domain "vozko/domain/media"
	"vozko/domain/mediagen"
)

func pixelAt(t *testing.T, video []byte, at string, x, y int) [3]byte {
	t.Helper()
	in := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(in, video, 0o600); err != nil {
		t.Fatal(err)
	}
	crop := "crop=2:2:" + strconv.Itoa(x) + ":" + strconv.Itoa(y)
	out, err := exec.Command("ffmpeg", "-v", "error", "-ss", at, "-i", in, "-frames:v", "1", "-vf", crop, "-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1").Output()
	if err != nil || len(out) < 3 {
		t.Fatalf("pixel at %s: %v", at, err)
	}
	return [3]byte{out[0], out[1], out[2]}
}

func isBlue(p [3]byte) bool  { return p[2] > 150 && p[0] < 100 }
func isBlack(p [3]byte) bool { return p[0] < 40 && p[1] < 40 && p[2] < 40 }

func renderAnimated(t *testing.T, keyframes *mediagen.Keyframes) []byte {
	t.Helper()
	requireFFmpeg(t)
	srv := mediaServer(t, map[string][]byte{
		"square.png": synthesize(t, "square.png", "-f", "lavfi", "-i", "color=c=blue:s=200x200", "-frames:v", "1", "-pix_fmt", "rgba"),
	})
	timeline := mediagen.Timeline{
		DurationMS: 3_000, Background: "#000000",
		Visual: []mediagen.Track{{Clips: []mediagen.Clip{{
			MediaID: "square", DurationMS: 3_000, Fit: mediagen.FitContain,
			Transform: mediagen.Transform{X: 0.5, Y: 0.5, W: 0.2, H: 0.2, Opacity: 1},
			Keyframes: keyframes,
		}}}},
	}
	req := mediagen.Request{Kind: mediagen.KindVideo, WorkspaceID: "ws", Aspect: mediagen.AspectSquare, Video: timeline}
	sources := []mediagen.Source{{MediaID: "square", URL: srv.URL + "/square.png", Type: media_domain.MediaTypeProductImage}}
	out, err := NewVideoRenderer(srv.Client()).Generate(context.Background(), req, sources)
	if err != nil {
		t.Fatal(err)
	}
	return out.Bytes
}

func TestPositionKeyframesMoveTheClipAcrossTheFrame(t *testing.T) {
	video := renderAnimated(t, &mediagen.Keyframes{X: []mediagen.Keyframe{
		{AtMS: 0, Value: 0.2, Easing: mediagen.EaseLinear}, {AtMS: 2_000, Value: 0.8, Easing: mediagen.EaseLinear},
	}})
	left, right := 216, 864
	if p := pixelAt(t, video, "0.1", left, 540); !isBlue(p) {
		t.Fatalf("starts on the left, got %v", p)
	}
	if p := pixelAt(t, video, "0.1", right, 540); !isBlack(p) {
		t.Fatalf("right is empty at the start, got %v", p)
	}
	if p := pixelAt(t, video, "2.5", right, 540); !isBlue(p) {
		t.Fatalf("ends on the right, got %v", p)
	}
	if p := pixelAt(t, video, "1.0", 540, 540); !isBlue(p) {
		t.Fatalf("passes the middle halfway, got %v", p)
	}
}

func TestEveryEasingHasItsOwnRenderCurve(t *testing.T) {
	for _, easing := range append(mediagen.Easings(), "cubic-bezier(0.05,0.7,0.1,1)") {
		if easing != mediagen.EaseLinear && easingExpr(easing, "p") == "p" {
			t.Errorf("%s renders as linear", easing)
		}
	}
}

func TestAnOvershootingEasingCarriesTheClipPastItsLastKey(t *testing.T) {
	video := renderAnimated(t, &mediagen.Keyframes{X: []mediagen.Keyframe{
		{AtMS: 0, Value: 0.2, Easing: mediagen.EaseBackOut}, {AtMS: 2_000, Value: 0.8, Easing: mediagen.EaseLinear},
	}})
	past := 990
	if p := pixelAt(t, video, "1.5", past, 540); !isBlue(p) {
		t.Fatalf("overshoots past the last key, got %v", p)
	}
	if p := pixelAt(t, video, "2.5", past, 540); !isBlack(p) {
		t.Fatalf("settles on the last key, got %v", p)
	}
}

func TestOpacityKeyframesFadeTheClipFrameByFrame(t *testing.T) {
	video := renderAnimated(t, &mediagen.Keyframes{Opacity: []mediagen.Keyframe{
		{AtMS: 0, Value: 0, Easing: mediagen.EaseHold}, {AtMS: 1_000, Value: 0.5, Easing: mediagen.EaseLinear}, {AtMS: 2_000, Value: 1, Easing: mediagen.EaseLinear},
	}})
	hidden, half, full := pixelAt(t, video, "0.5", 540, 540), pixelAt(t, video, "1.2", 540, 540), pixelAt(t, video, "2.5", 540, 540)
	if !isBlack(hidden) {
		t.Fatalf("held at zero, got %v", hidden)
	}
	if half[2] < 90 || half[2] > 190 {
		t.Fatalf("about half way, got %v", half)
	}
	if full[2] < 220 {
		t.Fatalf("fully opaque, got %v", full)
	}
}

func TestScaleAndRotationKeyframesGrowAndTurnTheClip(t *testing.T) {
	video := renderAnimated(t, &mediagen.Keyframes{
		Scale:    []mediagen.Keyframe{{AtMS: 0, Value: 1, Easing: mediagen.EaseInOut}, {AtMS: 2_000, Value: 3, Easing: mediagen.EaseLinear}},
		Rotation: []mediagen.Keyframe{{AtMS: 0, Value: 0, Easing: mediagen.EaseLinear}, {AtMS: 2_000, Value: 45, Easing: mediagen.EaseLinear}},
	})
	edge := 540 + 216
	if p := pixelAt(t, video, "0.1", edge, 540); !isBlack(p) {
		t.Fatalf("small at the start, got %v", p)
	}
	if p := pixelAt(t, video, "2.5", edge, 540); !isBlue(p) {
		t.Fatalf("three times larger at the end, got %v", p)
	}
	corner := 540 + 300
	if p := pixelAt(t, video, "2.5", corner, 540-300); !isBlack(p) {
		t.Fatalf("turned 45 degrees, so the old corner is empty, got %v", p)
	}
}
