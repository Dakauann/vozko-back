package media

// HoldMusicTranscoder standardizes an uploaded hold music file: any input format
// becomes a small MONO MP3 bounded in duration and bitrate, validated to be
// decodable by the telephony PCM loader before it is stored. Implemented in infra
// (ffmpeg); the port lives here so the upload use case stays infra free.
type HoldMusicTranscoder interface {
	// ToHoldMusicMP3 returns the standardized MP3 bytes, or an error when the
	// input is not decodable audio.
	ToHoldMusicMP3(input []byte) ([]byte, error)
}
