package media_infra

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const maxEncodedBytes = 200 << 20

func runFFmpegArgs(ctx context.Context, args ...string) error {
	return runFFmpegIn(ctx, "", args...)
}

func runFFmpegIn(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", append([]string{"-hide_banner", "-loglevel", "error", "-nostdin"}, args...)...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w, stderr: %s", err, stderr.String())
	}
	return nil
}

func readOutput(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 {
		return nil, errEmptyConversion
	}
	if info.Size() > maxEncodedBytes {
		return nil, fmt.Errorf("ffmpeg output larger than %d bytes", maxEncodedBytes)
	}
	return os.ReadFile(path)
}

func workDir() (string, func(), error) {
	dir, err := os.MkdirTemp("", "vozko-media-*")
	if err != nil {
		return "", nil, err
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

func writeInput(dir, name string, data []byte) (string, error) {
	path := filepath.Join(dir, name)
	return path, os.WriteFile(path, data, 0o600)
}
