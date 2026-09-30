package voip

import (
	"errors"
	"time"
)

const (
	PCMFrameDuration = 20 * time.Millisecond
	PCMFrameBytes    = PCMSampleRate / 50 * 2
)

var (
	ErrPlaybackStopped = errors.New("playback stopped")
	ErrNothingToPlay   = errors.New("no audio to play")
)

func PacePCM(pcm []byte, send func(frame []byte) error, stop <-chan struct{}, interrupted func() bool) (bool, error) {
	ticker := time.NewTicker(PCMFrameDuration)
	defer ticker.Stop()
	for offset := 0; offset < len(pcm); offset += PCMFrameBytes {
		select {
		case <-stop:
			return false, ErrPlaybackStopped
		default:
		}
		if interrupted() {
			return true, nil
		}
		if err := send(pcm[offset:min(offset+PCMFrameBytes, len(pcm))]); err != nil {
			return false, err
		}
		select {
		case <-ticker.C:
		case <-stop:
			return false, ErrPlaybackStopped
		}
	}
	return false, nil
}

func LoopPCM(pcm []byte, send func(frame []byte) error, stop <-chan struct{}) error {
	if len(pcm) == 0 {
		return ErrNothingToPlay
	}
	never := func() bool { return false }
	for {
		if _, err := PacePCM(pcm, send, stop, never); err != nil {
			return err
		}
	}
}
