package sip_trunk_usecase

import (
	"context"
	"errors"
	"time"

	"vozko/domain/callrouting"
	"vozko/domain/voip"
	"vozko/domain/workflow"
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
	interrupted, err := voip.PacePCM(pcm, c.line.SendAudio, c.line.Done(), func() bool {
		return interruptible && c.keys.Pressed()
	})
	if errors.Is(err, voip.ErrPlaybackStopped) || (err == nil && c.keys.Ended()) {
		return false, workflow.ErrCallEnded
	}
	return interrupted, err
}

func (c *liveVoiceCall) NextKey(timeout time.Duration) (rune, bool, error) {
	return c.keys.Next(timeout)
}

type routedVoiceCall struct {
	*liveVoiceCall
	routed callrouting.RoutedCall
	queues callrouting.QueueEntrance
}

var _ workflow.VoiceTransfers = (*routedVoiceCall)(nil)

func newRoutedVoiceCall(routed callrouting.RoutedCall, keys <-chan rune, queues callrouting.QueueEntrance) *routedVoiceCall {
	return &routedVoiceCall{liveVoiceCall: newLiveVoiceCall(routed, keys), routed: routed, queues: queues}
}

func (c *routedVoiceCall) TransferToQueue(ctx context.Context, transfer workflow.QueueTransfer) (bool, error) {
	if c.queues == nil {
		return false, workflow.ErrNotTransferable
	}
	outcome, err := c.queues.EnterQueue(ctx, callrouting.QueueEntry{
		WorkspaceID: c.routed.WorkspaceID(),
		QueueID:     transfer.QueueID,
		Notes:       transfer.Notes,
		From:        transfer.From,
		Call:        c.routed,
	})
	switch {
	case err != nil:
		return false, err
	case outcome == callrouting.OutcomeAbandoned:
		return false, workflow.ErrCallEnded
	}
	return outcome == callrouting.OutcomeConnected, nil
}
