package calls_usecase

import (
	"sync"

	"vozko/domain/conversation"
)

const recordingCRMCallForwardBuffer = 200

type RecordingCRMCall struct {
	inner    conversation.CRMCall
	recorder *CallRecorder
	pool     *RecordingUploadPool

	mu     sync.Mutex
	out    chan []byte
	tapped bool

	finishOnce sync.Once
}

func NewRecordingCRMCall(inner conversation.CRMCall, pool *RecordingUploadPool, workspaceID, entryID, leadID string) *RecordingCRMCall {
	if inner == nil || pool == nil {
		return nil
	}
	recorder := NewCallRecorder(inner.ID(), workspaceID, entryID, leadID)
	if recorder == nil {
		return nil
	}
	c := &RecordingCRMCall{
		inner:    inner,
		recorder: recorder,
		pool:     pool,
	}
	go c.watchDone()
	return c
}

func (c *RecordingCRMCall) ID() string { return c.inner.ID() }

func (c *RecordingCRMCall) SendAudio(pcm16 []byte) error {
	if len(pcm16) > 0 {
		c.recorder.RecordRemotePCM(pcm16, recorderSampleRate)
	}
	return c.inner.SendAudio(pcm16)
}

func (c *RecordingCRMCall) AudioStream() <-chan []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tapped {
		return c.out
	}
	c.tapped = true
	c.out = make(chan []byte, recordingCRMCallForwardBuffer)
	go c.pump(c.out)
	return c.out
}

func (c *RecordingCRMCall) pump(out chan<- []byte) {
	defer close(out)
	for pcm := range c.inner.AudioStream() {
		if len(pcm) > 0 {
			c.recorder.RecordCallerPCM(pcm)
		}
		select {
		case out <- pcm:
		default:
		}
	}
}

func (c *RecordingCRMCall) Events() <-chan conversation.CallEvent { return c.inner.Events() }

func (c *RecordingCRMCall) Done() <-chan struct{} { return c.inner.Done() }

func (c *RecordingCRMCall) Hangup() error {
	err := c.inner.Hangup()
	c.finish()
	return err
}

func (c *RecordingCRMCall) watchDone() {
	<-c.inner.Done()
	c.finish()
}

func (c *RecordingCRMCall) finish() {
	c.finishOnce.Do(func() {
		c.recorder.Finish(c.pool)
	})
}

var _ conversation.CRMCall = (*RecordingCRMCall)(nil)
