package media_infra

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	media_domain "vozko/domain/media"
	"vozko/domain/mediagen"
)

func mediaServer(t *testing.T, files map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func durationOf(t *testing.T, data []byte) float64 {
	t.Helper()
	line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(probe(t, data, "format=duration")), "duration="))
	d, err := strconv.ParseFloat(line, 64)
	if err != nil {
		t.Fatalf("duration %q: %v", line, err)
	}
	return d
}

func centerPixel(t *testing.T, video []byte, at string) [3]byte {
	t.Helper()
	in := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(in, video, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("ffmpeg", "-v", "error", "-ss", at, "-i", in, "-frames:v", "1", "-vf", "crop=2:2:(iw/2):(ih/2)", "-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1").Output()
	if err != nil || len(out) < 3 {
		t.Fatalf("pixel at %s: %v", at, err)
	}
	return [3]byte{out[0], out[1], out[2]}
}

func renderSources(t *testing.T) (*httptest.Server, []mediagen.Source) {
	t.Helper()
	srv := mediaServer(t, map[string][]byte{
		"photo.png":   synthesize(t, "photo.png", "-f", "lavfi", "-i", "color=c=red:s=640x480", "-frames:v", "1"),
		"clip.mp4":    synthesize(t, "clip.mp4", "-f", "lavfi", "-i", "testsrc=s=320x240:d=4", "-pix_fmt", "yuv420p"),
		"talk.mp4":    synthesize(t, "talk.mp4", "-f", "lavfi", "-i", "testsrc=s=160x120:d=3", "-f", "lavfi", "-i", "sine=frequency=500:duration=3", "-pix_fmt", "yuv420p", "-shortest"),
		"overlay.png": synthesize(t, "overlay.png", "-f", "lavfi", "-i", "color=c=blue@0.9:s=200x200", "-frames:v", "1", "-pix_fmt", "rgba"),
		"song.m4a":    synthesize(t, "song.m4a", "-f", "lavfi", "-i", "sine=frequency=330:duration=6", "-c:a", "aac"),
	})
	sources := []mediagen.Source{
		{MediaID: "photo", URL: srv.URL + "/photo.png", Type: media_domain.MediaTypeProductImage},
		{MediaID: "clip", URL: srv.URL + "/clip.mp4", Type: media_domain.MediaTypeProductVideo},
		{MediaID: "talk", URL: srv.URL + "/talk.mp4", Type: media_domain.MediaTypeProductVideo},
		{MediaID: "overlay", URL: srv.URL + "/overlay.png", Type: media_domain.MediaTypeProductImage},
		{MediaID: "song", URL: srv.URL + "/song.m4a", Type: media_domain.MediaTypeAudio},
	}
	return srv, sources
}

func editedTimeline() mediagen.Timeline {
	full := mediagen.FullFrame()
	return mediagen.Timeline{
		DurationMS: 5_000, Background: "#000000",
		Visual: []mediagen.Track{
			{Clips: []mediagen.Clip{
				{MediaID: "photo", StartMS: 0, DurationMS: 2_000, Fit: mediagen.FitCover, Transform: full, FadeOutMS: 200},
				{MediaID: "clip", StartMS: 2_000, DurationMS: 3_000, TrimInMS: 500, Fit: mediagen.FitContain, Transform: full},
			}},
			{Clips: []mediagen.Clip{
				{MediaID: "overlay", StartMS: 1_000, DurationMS: 2_000, Fit: mediagen.FitContain,
					Transform: mediagen.Transform{X: 0.5, Y: 0.5, W: 0.3, H: 0.2, Rotation: 15, Opacity: 1}},
			}},
		},
		Audio: []mediagen.Track{
			{Clips: []mediagen.Clip{{MediaID: "song", DurationMS: 5_000, Volume: 0.25, FadeOutMS: 1_500}}},
			{Clips: []mediagen.Clip{{MediaID: "talk", StartMS: 1_000, DurationMS: 2_000, TrimInMS: 500, Volume: 1, FadeInMS: 100}}},
		},
	}
}

func TestATimelineIsRenderedWithItsClipsOverlaysAndSound(t *testing.T) {
	requireFFmpeg(t)
	srv, sources := renderSources(t)
	req := mediagen.Request{Kind: mediagen.KindVideo, WorkspaceID: "ws", Aspect: mediagen.AspectStory, Video: editedTimeline()}
	out, err := NewVideoRenderer(srv.Client()).Generate(context.Background(), req, sources)
	if err != nil {
		t.Fatal(err)
	}
	info := probe(t, out.Bytes, "stream=codec_name,width,height")
	for _, want := range []string{"codec_name=h264", "width=1080", "height=1920", "codec_name=aac"} {
		if !strings.Contains(info, want) {
			t.Fatalf("missing %s in %s", want, info)
		}
	}
	if d := durationOf(t, out.Bytes); d < 4.9 || d > 5.2 {
		t.Fatalf("duration %.2f, want 5 s", d)
	}
	before, during := centerPixel(t, out.Bytes, "0.5"), centerPixel(t, out.Bytes, "1.5")
	if before[0] < 180 || before[2] > 80 {
		t.Fatalf("before the overlay the photo shows, got %v", before)
	}
	if during[2] < 150 || during[0] > 100 {
		t.Fatalf("inside its window the overlay is on top, got %v", during)
	}
	if out.MIMEType != "video/mp4" || out.CostReported {
		t.Fatalf("output %+v", out)
	}
}

func TestASilentVideoOnAnAudioTrackIsSilence(t *testing.T) {
	requireFFmpeg(t)
	srv, sources := renderSources(t)
	timeline := editedTimeline()
	timeline.Audio[1].Clips[0].MediaID = "clip"
	req := mediagen.Request{Kind: mediagen.KindVideo, WorkspaceID: "ws", Aspect: mediagen.AspectStory, Video: timeline}
	out, err := NewVideoRenderer(srv.Client()).Generate(context.Background(), req, sources)
	if err != nil {
		t.Fatal(err)
	}
	if d := durationOf(t, out.Bytes); d < 4.9 || d > 5.2 {
		t.Fatalf("duration %.2f", d)
	}
}

func TestARenderRefusesSourcesThatAreNotInTheTimeline(t *testing.T) {
	requireFFmpeg(t)
	req := mediagen.Request{Kind: mediagen.KindVideo, WorkspaceID: "ws", Aspect: mediagen.AspectSquare, Video: editedTimeline()}
	if _, err := NewVideoRenderer(http.DefaultClient).Generate(context.Background(), req, nil); !errors.Is(err, errRenderSources) {
		t.Fatalf("got %v", err)
	}
}
