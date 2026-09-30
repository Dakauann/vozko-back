package sip_trunk_usecase

import (
	"time"

	"vozko/domain/voip"
	"vozko/domain/workflow"
)

const (
	pcmFrameDuration = 20 * time.Millisecond
	pcmFrameBytes    = voip.PCMSampleRate / 50 * 2
)

type audioLine interface {
	SendAudio(pcm []byte) error
	Done() <-chan struct{}
}

type liveVoiceCall struct {
	line audioLine
	keys *workflow.KeyQueue
}

var _ workflow.VoiceCall = (*liveVoiceCall)(nil)

func newLiveVoiceCall(line audioLine, keys <-chan rune) *liveVoiceCall {
	return &liveVoiceCall{line: line, keys: workflow.NewKeyQueue(keys, line.Done())}
}

func (c *liveVoiceCall) Play(pcm []byte, interruptible bool) (bool, error) {
	ticker := time.NewTicker(pcmFrameDuration)
	defer ticker.Stop()
	for offset := 0; offset < len(pcm); offset += pcmFrameBytes {
		if interruptible && c.keys.Pressed() {
			return true, nil
		}
		end := min(offset+pcmFrameBytes, len(pcm))
		if err := c.line.SendAudio(pcm[offset:end]); err != nil {
			return false, err
		}
		select {
		case <-ticker.C:
		case <-c.line.Done():
			return false, workflow.ErrCallEnded
		}
	}
	if c.keys.Ended() {
		return false, workflow.ErrCallEnded
	}
	return false, nil
}

func (c *liveVoiceCall) NextKey(timeout time.Duration) (rune, bool, error) {
	return c.keys.Next(timeout)
}
