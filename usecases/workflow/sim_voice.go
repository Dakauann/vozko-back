package workflow_usecase

import (
	"context"
	"strings"
	"time"

	media_domain "vozko/domain/media"
	"vozko/domain/workflow"
)

type simVoiceAudio struct {
	media  media_domain.MediaRepository
	onPlay func(m *media_domain.Media)
}

func newSimVoiceAudio(media media_domain.MediaRepository, onPlay func(m *media_domain.Media)) *simVoiceAudio {
	return &simVoiceAudio{media: media, onPlay: onPlay}
}

func (a *simVoiceAudio) LoadPCM(_ context.Context, workspaceID, mediaID string) ([]byte, error) {
	if a.media == nil {
		return nil, workflow.ErrAudioNotPlayable
	}
	m, err := a.media.GetMediaByID(strings.TrimSpace(mediaID))
	if err != nil || !m.PlayableOnCallFor(workspaceID) {
		return nil, workflow.ErrAudioNotPlayable
	}
	a.onPlay(m)
	return nil, nil
}

type simVoiceCall struct {
	keys       *workflow.KeyQueue
	onWait     func(timeout time.Duration)
	onTransfer func(transfer workflow.QueueTransfer) error
}

func newSimVoiceCall(keys <-chan rune, ended <-chan struct{}, onWait func(timeout time.Duration), onTransfer func(transfer workflow.QueueTransfer) error) *simVoiceCall {
	return &simVoiceCall{keys: workflow.NewKeyQueue(keys, ended), onWait: onWait, onTransfer: onTransfer}
}

func (c *simVoiceCall) TransferToQueue(_ context.Context, transfer workflow.QueueTransfer) (bool, error) {
	if c.keys.Ended() {
		return false, workflow.ErrCallEnded
	}
	if c.onTransfer == nil {
		return false, workflow.ErrNotTransferable
	}
	if err := c.onTransfer(transfer); err != nil {
		return false, err
	}
	return true, nil
}

func (c *simVoiceCall) Play(_ []byte, interruptible bool) (bool, error) {
	if c.keys.Ended() {
		return false, workflow.ErrCallEnded
	}
	return interruptible && c.keys.Pressed(), nil
}

func (c *simVoiceCall) NextKey(timeout time.Duration) (rune, bool, error) {
	if !c.keys.Pressed() {
		c.onWait(timeout)
	}
	return c.keys.Next(timeout)
}

const (
	testKeyMock          = "key"
	testTransferMock     = "transfer"
	testTransferTimedOut = "timeout"
)

type scriptedVoiceCall struct {
	key      string
	transfer string
}

func (c scriptedVoiceCall) TransferToQueue(context.Context, workflow.QueueTransfer) (bool, error) {
	return c.transfer != testTransferTimedOut, nil
}

func (c scriptedVoiceCall) Play([]byte, bool) (bool, error) {
	return false, nil
}

func (c scriptedVoiceCall) NextKey(time.Duration) (rune, bool, error) {
	if !workflow.IsDTMFKey(c.key) {
		return 0, false, nil
	}
	return rune(c.key[0]), true, nil
}

func testVoiceRuntime(wf *workflow.Workflow, mockedState map[string]interface{}) interface{} {
	if wf == nil || wf.Type != workflow.WorkflowTypeVoice {
		return nil
	}
	key, _ := mockedState[testKeyMock].(string)
	transfer, _ := mockedState[testTransferMock].(string)
	return scriptedVoiceCall{key: strings.TrimSpace(key), transfer: strings.TrimSpace(transfer)}
}
