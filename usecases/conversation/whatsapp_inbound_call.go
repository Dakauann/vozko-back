package conversation_usecase

import (
	"context"
	"log"
	"sync"
	"time"

	conversation_domain "vozko/domain/conversation"
	"vozko/infra/conversation/whatsapp/media"
)

type whatsappInboundCall struct {
	id        string
	phoneID   string
	callID    string
	from      string
	media     *media.Session
	signaling conversation_domain.WhatsAppCallSignaling
	log       *log.Logger

	events  chan conversation_domain.CallEvent
	audioIn chan []byte
	done    chan struct{}

	ctx    context.Context
	cancel context.CancelFunc

	startOnce    sync.Once
	teardownOnce sync.Once
	hangupOnce   sync.Once
}

func newWhatsAppInboundCall(
	id, phoneID, callID, from string,
	mediaSess *media.Session,
	signaling conversation_domain.WhatsAppCallSignaling,
	logger *log.Logger,
) *whatsappInboundCall {
	if logger == nil {
		logger = log.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &whatsappInboundCall{
		id:        id,
		phoneID:   phoneID,
		callID:    callID,
		from:      from,
		media:     mediaSess,
		signaling: signaling,
		log:       logger,
		events:    make(chan conversation_domain.CallEvent, 8),
		audioIn:   make(chan []byte, 200),
		done:      make(chan struct{}),
		ctx:       ctx,
		cancel:    cancel,
	}
}

func (c *whatsappInboundCall) ID() string                                   { return c.id }
func (c *whatsappInboundCall) AudioStream() <-chan []byte                   { return c.audioIn }
func (c *whatsappInboundCall) Events() <-chan conversation_domain.CallEvent { return c.events }
func (c *whatsappInboundCall) Done() <-chan struct{}                        { return c.done }

func (c *whatsappInboundCall) SendAudio(pcm16 []byte) error {
	if c.media == nil {
		return nil
	}
	return c.media.WritePCM(pcm16)
}

func (c *whatsappInboundCall) Start() {
	c.startOnce.Do(func() {
		select {
		case c.events <- conversation_domain.CallEvent{Type: conversation_domain.CallEventAnswered}:
		default:
		}
		go c.pipeIncomingAudio()
	})
}

func (c *whatsappInboundCall) pipeIncomingAudio() {
	defer func() {
		close(c.done)

		time.Sleep(50 * time.Millisecond)
		close(c.audioIn)
		close(c.events)
	}()
	for {
		select {
		case <-c.ctx.Done():
			return
		case pcm, ok := <-c.media.ReadChan():
			if !ok {
				return
			}
			select {
			case c.audioIn <- pcm:
			default:
			}
		}
	}
}

func (c *whatsappInboundCall) markEnded(reason string) {
	c.log.Printf("[WAInboundCall] %s ended by remote (reason=%q)", c.id, reason)
	c.teardown()
}

func (c *whatsappInboundCall) Hangup() error {
	var err error
	c.hangupOnce.Do(func() {
		if c.signaling != nil && c.callID != "" {
			err = c.signaling.TerminateCall(context.Background(), c.phoneID, c.callID)
		}
		c.teardown()
	})
	return err
}

func (c *whatsappInboundCall) teardown() {
	c.teardownOnce.Do(func() {
		c.cancel()
		if c.media != nil {
			_ = c.media.Close()
		}
	})
}

var _ conversation_domain.CRMCall = (*whatsappInboundCall)(nil)
