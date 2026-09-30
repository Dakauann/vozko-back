package sip_trunk_usecase

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/sip_trunk"
	"vozko/domain/voip"
)

const (
	trunkCallEventBuffer = 8
	trunkCallAudioBuffer = 64
	trunkHangupTimeout   = 5 * time.Second
)

type trunkCall struct {
	id      string
	trunkID string
	engine  sip_trunk.Engine

	events chan conversation.CallEvent
	audio  chan []byte
	done   chan struct{}
	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	session *sip_trunk.TrunkCallSession

	listenOnce sync.Once
	listening  chan struct{}
}

var _ conversation.CRMCall = (*trunkCall)(nil)

func newTrunkCall(id, trunkID string, engine sip_trunk.Engine) *trunkCall {
	ctx, cancel := context.WithCancel(context.Background())
	return &trunkCall{
		id:        id,
		trunkID:   trunkID,
		engine:    engine,
		events:    make(chan conversation.CallEvent, trunkCallEventBuffer),
		audio:     make(chan []byte, trunkCallAudioBuffer),
		done:      make(chan struct{}),
		listening: make(chan struct{}),
		ctx:       ctx,
		cancel:    cancel,
	}
}

func (c *trunkCall) ID() string                            { return c.id }
func (c *trunkCall) AudioStream() <-chan []byte            { return c.audio }
func (c *trunkCall) Events() <-chan conversation.CallEvent { return c.events }
func (c *trunkCall) Done() <-chan struct{}                 { return c.done }

func (c *trunkCall) SendAudio(pcm []byte) error {
	session := c.currentSession()
	if session == nil {
		return nil
	}
	select {
	case <-c.done:
		return nil
	default:
	}
	if err := session.Audio.WritePCM(pcm); err != nil {
		select {
		case <-session.Audio.Done():
			return nil
		default:
			return err
		}
	}
	return nil
}

func (c *trunkCall) Hangup() error {
	c.cancel()
	session := c.currentSession()
	if session == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), trunkHangupTimeout)
	defer cancel()
	if err := c.engine.Hangup(ctx, c.trunkID, session.ID); err != nil && !errors.Is(err, sip_trunk.ErrCallNotFound) {
		return err
	}
	return nil
}

func (c *trunkCall) currentSession() *sip_trunk.TrunkCallSession {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session
}

func (c *trunkCall) dial(number string) {
	c.emit(conversation.CallEvent{Type: conversation.CallEventRinging})
	session, err := c.engine.Invite(c.ctx, c.trunkID, sip_trunk.TrunkInviteInput{PhoneNumber: number, Progress: c})
	if err != nil {
		c.finish(dialOutcome(c.ctx, err))
		return
	}
	c.bridge(session)
}

func (c *trunkCall) bridge(session sip_trunk.TrunkCallSession) {
	c.mu.Lock()
	c.session = &session
	c.mu.Unlock()
	if c.ctx.Err() != nil {
		_ = c.Hangup()
		c.finish(conversation.CallEvent{Type: conversation.CallEventEnded, Reason: "cancelled"})
		return
	}
	c.emit(conversation.CallEvent{Type: conversation.CallEventAnswered})
	c.listen(session.Audio)
	<-c.listening
	c.finish(conversation.CallEvent{Type: conversation.CallEventEnded})
}

func (c *trunkCall) Alerting() {
	c.emit(conversation.CallEvent{Type: conversation.CallEventAlerting})
}

func (c *trunkCall) EarlyMedia(audio voip.PCMStream) {
	c.listen(audio)
}

func (c *trunkCall) listen(audio voip.PCMStream) {
	c.listenOnce.Do(func() {
		go func() {
			defer close(c.listening)
			for {
				select {
				case <-c.ctx.Done():
					return
				case frame, ok := <-audio.Frames():
					if !ok {
						return
					}
					c.forward(frame)
				}
			}
		}()
	})
}

func (c *trunkCall) forward(frame []byte) {
	for {
		select {
		case c.audio <- frame:
			return
		default:
		}
		select {
		case <-c.audio:
		default:
		}
	}
}

func (c *trunkCall) emit(event conversation.CallEvent) {
	c.events <- event
}

func (c *trunkCall) finish(event conversation.CallEvent) {
	c.emit(event)
	c.cancel()
	c.listenOnce.Do(func() { close(c.listening) })
	<-c.listening
	close(c.done)
	close(c.events)
	close(c.audio)
}

func dialOutcome(ctx context.Context, err error) conversation.CallEvent {
	if ctx.Err() != nil {
		return conversation.CallEvent{Type: conversation.CallEventEnded, Reason: "cancelled"}
	}
	var rejected *sip_trunk.CallRejectedError
	if errors.As(err, &rejected) {
		reason := fmt.Sprintf("%d %s", rejected.StatusCode, rejected.Reason)
		switch rejected.StatusCode {
		case 486, 600:
			return conversation.CallEvent{Type: conversation.CallEventBusy, Reason: reason}
		case 408, 480, 487:
			return conversation.CallEvent{Type: conversation.CallEventNoAnswer, Reason: reason}
		case 603:
			return conversation.CallEvent{Type: conversation.CallEventDeclined, Reason: reason}
		}
		return conversation.CallEvent{Type: conversation.CallEventFailed, Reason: reason}
	}
	return conversation.CallEvent{Type: conversation.CallEventFailed, Reason: err.Error()}
}

func newAnsweredTrunkCall(id, trunkID string, engine sip_trunk.Engine, session sip_trunk.TrunkCallSession) *trunkCall {
	call := newTrunkCall(id, trunkID, engine)
	call.session = &session
	return call
}

func (c *trunkCall) Start() {
	session := c.currentSession()
	if session == nil {
		return
	}
	go c.bridge(*session)
}
