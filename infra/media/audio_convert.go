package media_infra

import (
	"bytes"
	"fmt"
	"os/exec"

	media_domain "vozko/domain/media"
	voip_audio "vozko/infra/voip/audio"
)

// holdMusicMaxSeconds bounds a hold music loop: long uploads are trimmed, so a
// 25MB podcast can never become the hold path's decode payload.
const holdMusicMaxSeconds = 300

// TranscodeToHoldMusicMP3 standardizes any uploaded audio into the hold music
// format: mono, 22.05kHz, 48kbps MP3, at most 5 minutes. The call media plane is
// 8kHz G.711, so this keeps far more fidelity than the wire can carry while
// shrinking files roughly tenfold.
func TranscodeToHoldMusicMP3(input []byte) ([]byte, error) {
	cmd := exec.Command("ffmpeg",
		"-nostdin", "-hide_banner", "-loglevel", "error",
		"-i", "pipe:0",
		"-vn", "-sn", "-dn",
		"-ac", "1",
		"-ar", "22050",
		"-codec:a", "libmp3lame",
		"-b:a", "48k",
		"-t", fmt.Sprintf("%d", holdMusicMaxSeconds),
		"-f", "mp3",
		"pipe:1",
	)

	cmd.Stdin = bytes.NewReader(input)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg hold music conversion failed: %w, stderr: %s", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// HoldMusicTranscoder implements the domain port: ffmpeg standardization plus a
// decode round trip through the SAME loader the hold path uses, so a track that
// stores is a track that provably plays.
type HoldMusicTranscoder struct{}

func NewHoldMusicTranscoder() HoldMusicTranscoder { return HoldMusicTranscoder{} }

func (HoldMusicTranscoder) ToHoldMusicMP3(input []byte) ([]byte, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("hold music: empty upload")
	}
	out, err := TranscodeToHoldMusicMP3(input)
	if err != nil {
		return nil, err
	}
	pcm, err := voip_audio.DecodeMP3AsTelephonyPCM(bytes.NewReader(out))
	if err != nil {
		return nil, fmt.Errorf("hold music: transcoded file failed the playback decode check: %w", err)
	}
	if len(pcm) < 320 { // under one 20ms telephony frame
		return nil, fmt.Errorf("hold music: audio too short to loop")
	}
	return out, nil
}

var _ media_domain.HoldMusicTranscoder = HoldMusicTranscoder{}

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
