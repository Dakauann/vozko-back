package media_infra

import (
	"bytes"
	"fmt"
	"os/exec"
)

func ConvertPCMToOGG(pcmData []byte, sampleRate int) ([]byte, error) {
	cmd := exec.Command("ffmpeg",
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

	cmd.Stdin = bytes.NewReader(pcmData)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg conversion failed: %w, stderr: %s", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

func ConvertToOGGOpus(audioData []byte) ([]byte, error) {
	cmd := exec.Command("ffmpeg",
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
	cmd.Stdin = bytes.NewReader(audioData)

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg conversion failed: %w, stderr: %s", err, stderr.String())
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("ffmpeg produced empty output")
	}
	return stdout.Bytes(), nil
}
