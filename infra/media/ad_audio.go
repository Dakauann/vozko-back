package media_infra

import (
	"context"
	"path/filepath"
)

const (
	adLoudness    = "loudnorm=I=-16:TP=-1.5:LRA=11"
	speechCleanup = "highpass=f=80,afftdn=nf=-25"
)

func EncodeAdAudio(ctx context.Context, raw []byte) ([]byte, error) {
	return encodeAudio(ctx, raw, adLoudness)
}

func CleanAudio(ctx context.Context, raw []byte) ([]byte, error) {
	return encodeAudio(ctx, raw, speechCleanup+","+adLoudness)
}

func encodeAudio(ctx context.Context, raw []byte, filters string) ([]byte, error) {
	dir, cleanup, err := workDir()
	if err != nil {
		return nil, err
	}
	defer cleanup()
	in, err := writeInput(dir, "source", raw)
	if err != nil {
		return nil, err
	}
	out := filepath.Join(dir, "audio.m4a")
	if err := runFFmpegArgs(ctx,
		"-i", in,
		"-vn",
		"-af", filters,
		"-ar", "48000",
		"-ac", "2",
		"-c:a", "aac",
		"-b:a", "192k",
		"-movflags", "+faststart",
		"-y", out,
	); err != nil {
		return nil, err
	}
	return readOutput(out)
}
