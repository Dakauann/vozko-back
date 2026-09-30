package media_infra

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
)

var errEmptyConversion = errors.New("ffmpeg produced empty output")

func runFFmpeg(input []byte, args ...string) ([]byte, error) {
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stdin = bytes.NewReader(input)

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg conversion failed: %w, stderr: %s", err, stderr.String())
	}
	if stdout.Len() == 0 {
		return nil, errEmptyConversion
	}
	return stdout.Bytes(), nil
}

func ConvertPCMToOGG(pcmData []byte, sampleRate int) ([]byte, error) {
	return runFFmpeg(pcmData,
		"-f", "s16le",
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-ac", "1",
		"-i", "pipe:0",
		"-c:a", "libopus",
		"-b:a", "24k",
		"-application", "voip",
		"-f", "ogg",
		"pipe:1",
	)
}

func ConvertToOGGOpus(audioData []byte) ([]byte, error) {
	return runFFmpeg(audioData,
		"-hide_banner",
		"-loglevel", "error",
		"-i", "pipe:0",
		"-vn",
		"-map", "0:a:0",
		"-c:a", "libopus",
		"-b:a", "48k",
		"-ar", "48000",
		"-ac", "1",
		"-application", "voip",
		"-frame_duration", "20",
		"-f", "ogg",
		"pipe:1",
	)
}

func ConvertToPCM(audioData []byte, sampleRate int) ([]byte, error) {
	return runFFmpeg(audioData,
		"-hide_banner",
		"-loglevel", "error",
		"-i", "pipe:0",
		"-vn",
		"-map", "0:a:0",
		"-ac", "1",
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-f", "s16le",
		"pipe:1",
	)
}
