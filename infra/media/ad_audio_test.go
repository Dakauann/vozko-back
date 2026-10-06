package media_infra

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireFFmpeg(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " not installed")
		}
	}
}

func synthesize(t *testing.T, name string, args ...string) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("ffmpeg", append(append([]string{"-hide_banner", "-loglevel", "error"}, args...), "-y", out)...)
	if res, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("synthesize %s: %v %s", name, err, res)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func probe(t *testing.T, data []byte, entries string) string {
	t.Helper()
	in := filepath.Join(t.TempDir(), "probe.bin")
	if err := os.WriteFile(in, data, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", entries, "-of", "default=noprint_wrappers=1", in).Output()
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	return string(out)
}

func TestAdAudioIsStereoAACInAnM4A(t *testing.T) {
	requireFFmpeg(t)
	wav := synthesize(t, "tone.wav", "-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-ac", "1")
	encoded, err := EncodeAdAudio(context.Background(), wav)
	if err != nil {
		t.Fatal(err)
	}
	info := probe(t, encoded, "stream=codec_name,channels,sample_rate:format=format_name")
	for _, want := range []string{"codec_name=aac", "channels=2", "sample_rate=48000", "mp4"} {
		if !strings.Contains(info, want) {
			t.Fatalf("missing %s in %s", want, info)
		}
	}
}

func TestAdAudioRefusesWhatIsNotAudio(t *testing.T) {
	requireFFmpeg(t)
	if _, err := EncodeAdAudio(context.Background(), []byte("not audio at all")); err == nil {
		t.Fatal("garbage was encoded")
	}
}
