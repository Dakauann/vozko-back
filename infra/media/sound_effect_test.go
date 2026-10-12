package media_infra

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

func probedLength(t *testing.T, data []byte) float64 {
	t.Helper()
	line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(probe(t, data, "format=duration")), "duration="))
	seconds, err := strconv.ParseFloat(line, 64)
	if err != nil {
		t.Fatalf("duration %q: %v", line, err)
	}
	return seconds
}

func TestASoundEffectLosesItsSilentEdgesAndNeverOutlastsItsLength(t *testing.T) {
	requireFFmpeg(t)
	padded := synthesize(t, "padded.wav", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono:d=0.4", "-f", "lavfi", "-i", "sine=frequency=880:duration=0.8", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono:d=1.5",
		"-filter_complex", "[0][1][2]concat=n=3:v=0:a=1")
	encoded, err := EncodeSoundEffect(context.Background(), padded, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := probedLength(t, encoded); got < 0.7 || got > 1.0 {
		t.Fatalf("the silence around the sound stays: %.3f s", got)
	}
	info := probe(t, encoded, "stream=codec_name,channels,sample_rate:format=format_name")
	for _, want := range []string{"codec_name=aac", "channels=2", "sample_rate=48000", "mp4"} {
		if !strings.Contains(info, want) {
			t.Fatalf("missing %s in %s", want, info)
		}
	}
	long := synthesize(t, "long.wav", "-f", "lavfi", "-i", "sine=frequency=440:duration=4", "-ac", "1")
	capped, err := EncodeSoundEffect(context.Background(), long, 1500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got := probedLength(t, capped); got < 1.4 || got > 1.56 {
		t.Fatalf("an effect longer than asked is cut at its length, got %.3f s", got)
	}
}

func TestASilentSoundEffectIsAFailure(t *testing.T) {
	requireFFmpeg(t)
	silent := synthesize(t, "silent.wav", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono:d=2")
	if _, err := EncodeSoundEffect(context.Background(), silent, 2*time.Second); !errors.Is(err, ErrSilentSound) {
		t.Fatalf("got %v", err)
	}
	if _, err := EncodeSoundEffect(context.Background(), []byte("not audio at all"), 2*time.Second); err == nil {
		t.Fatal("garbage was encoded")
	}
}
