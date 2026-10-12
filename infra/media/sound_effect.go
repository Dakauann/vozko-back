package media_infra

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var ErrSilentSound = errors.New("media: the sound effect is silent")

const (
	soundEdgeSilence = "silenceremove=start_periods=1:start_threshold=-50dB:start_silence=0.02"
	soundFadeOut     = 50 * time.Millisecond
)

func EncodeSoundEffect(ctx context.Context, raw []byte, length time.Duration) ([]byte, error) {
	if length <= soundFadeOut {
		return nil, fmt.Errorf("media: a sound effect must be longer than %s", soundFadeOut)
	}
	filters := strings.Join([]string{
		soundEdgeSilence, "areverse", soundEdgeSilence, "areverse",
		"atrim=end=" + seconds(length.Milliseconds()),
		"afade=t=out:st=" + seconds((length - soundFadeOut).Milliseconds()) + ":d=" + seconds(soundFadeOut.Milliseconds()),
		adLoudness,
	}, ",")
	return encodeVerifiedAudio(ctx, raw, filters, audible)
}

func audible(ctx context.Context, path string) error {
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path).Output()
	if err != nil {
		return fmt.Errorf("media: probe the sound effect: %w", err)
	}
	length, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || length <= 0 {
		return ErrSilentSound
	}
	return nil
}
