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

// ConvertToOGGOpus re-encodes arbitrary audio into the ogg/opus a WhatsApp voice
// note must be.
//
// Every channel needs this and none of them can skip it: WhatsApp voice notes
// are opus, while the CRM's recorder hands us WAV (it records opus in the
// browser and transcodes to WAV so the waveform and playback work everywhere).
// Sending the WAV through unconverted is what "audio/wav is not accepted"
// really meant — the file was never in a shape WhatsApp would take.
//
// The settings are voice settings, not music: 48kHz mono at 48kbps with the
// voip profile and 20ms frames, which is what a phone produces and what keeps a
// two-minute note small enough to send on a bad connection.
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
