package media_infra

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	media_domain "vozko/domain/media"
	"vozko/domain/mediagen"
)

func proxySource(t *testing.T, name string, args ...string) (*http.Client, []mediagen.Source) {
	t.Helper()
	srv := mediaServer(t, map[string][]byte{name: synthesize(t, name, args...)})
	return srv.Client(), []mediagen.Source{{MediaID: "clip", URL: srv.URL + "/" + name, Type: media_domain.MediaTypeProductVideo}}
}

func keyframeTimes(t *testing.T, data []byte) []string {
	t.Helper()
	in := filepath.Join(t.TempDir(), "proxy.mp4")
	if err := os.WriteFile(in, data, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v", "-skip_frame", "nokey", "-show_entries", "frame=pts_time", "-of", "csv=p=0", in).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(out))
}

func TestAProxyIsA720pVideoWithAKeyframeEveryHalfSecondAndItsSound(t *testing.T) {
	requireFFmpeg(t)
	client, sources := proxySource(t, "wide.mp4", "-f", "lavfi", "-i", "testsrc=s=1920x1080:d=3:r=25", "-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-pix_fmt", "yuv420p", "-g", "250", "-shortest")
	out, err := NewProxyGenerator(client).Generate(context.Background(), mediagen.Request{}, sources)
	if err != nil {
		t.Fatal(err)
	}
	info := probe(t, out.Bytes, "stream=codec_name,width,height,r_frame_rate")
	for _, want := range []string{"codec_name=h264", "width=1280", "height=720", "r_frame_rate=30/1", "codec_name=aac"} {
		if !strings.Contains(info, want) {
			t.Fatalf("proxy lacks %s: %s", want, info)
		}
	}
	if keys := keyframeTimes(t, out.Bytes); len(keys) < 6 {
		t.Fatalf("expected a keyframe every half second, got %v", keys)
	}
	if out.MIMEType != "video/mp4" {
		t.Fatalf("mime %s", out.MIMEType)
	}
}

func TestAVerticalSilentClipKeepsItsShapeAndStaysSilent(t *testing.T) {
	requireFFmpeg(t)
	client, sources := proxySource(t, "tall.mp4", "-f", "lavfi", "-i", "testsrc=s=1081x1921:d=1", "-pix_fmt", "yuv444p")
	out, err := NewProxyGenerator(client).Generate(context.Background(), mediagen.Request{}, sources)
	if err != nil {
		t.Fatal(err)
	}
	info := probe(t, out.Bytes, "stream=codec_type,width,height")
	if !strings.Contains(info, "width=720") || !strings.Contains(info, "height=1280") || strings.Contains(info, "codec_type=audio") {
		t.Fatalf("info %s", info)
	}
}

func TestASmallClipIsNotEnlarged(t *testing.T) {
	requireFFmpeg(t)
	client, sources := proxySource(t, "small.mp4", "-f", "lavfi", "-i", "testsrc=s=640x360:d=1", "-pix_fmt", "yuv420p")
	out, err := NewProxyGenerator(client).Generate(context.Background(), mediagen.Request{}, sources)
	if err != nil {
		t.Fatal(err)
	}
	if info := probe(t, out.Bytes, "stream=width,height"); !strings.Contains(info, "width=640") || !strings.Contains(info, "height=360") {
		t.Fatalf("info %s", info)
	}
}
