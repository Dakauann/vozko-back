package conversation_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	conversation_domain "vozko/domain/conversation"
	callpermission "vozko/domain/whatsapp/call_permission"
	"vozko/infra/conversation/whatsapp/media"
)

type WhatsAppCallSource struct {
	signaling     conversation_domain.WhatsAppCallSignaling
	registry      conversation_domain.WhatsAppCallRegistry
	permissions   callpermission.Repository
	publicMediaIP string
	stunServers   []string
	log           *log.Logger
}

func NewWhatsAppCallSource(
	signaling conversation_domain.WhatsAppCallSignaling,
	registry conversation_domain.WhatsAppCallRegistry,
	permissions callpermission.Repository,
	publicMediaIP string,
	stunServers []string,
	logger *log.Logger,
) *WhatsAppCallSource {
	if logger == nil {
		logger = log.Default()
	}
	return &WhatsAppCallSource{
		signaling:     signaling,
		registry:      registry,
		permissions:   permissions,
		publicMediaIP: strings.TrimSpace(publicMediaIP),
		stunServers:   stunServers,
		log:           logger,
	}
}

func (s *WhatsAppCallSource) Name() string { return "whatsapp" }

func (s *WhatsAppCallSource) Dial(ctx context.Context, input conversation_domain.CallDialInput) (conversation_domain.CRMCall, error) {
	if s == nil || s.signaling == nil || s.registry == nil {
		return nil, errors.New("whatsapp call source not configured")
	}
	if strings.TrimSpace(input.WhatsAppPhoneID) == "" {
		return nil, errors.New("whatsapp business phone id is required")
	}
	if strings.TrimSpace(input.PhoneNumber) == "" {
		return nil, errors.New("phone number is required")
	}

	if s.permissions != nil {
		perm, err := s.permissions.FindByPhoneAndNumber(input.WhatsAppPhoneID, input.PhoneNumber)
		if err != nil {
			s.log.Printf("[WACall] permission lookup failed for phone %s: %v", input.WhatsAppPhoneID, err)
		} else if !perm.IsActive(time.Now()) {
			return nil, conversation_domain.ErrWhatsAppCallNoPermission
		}
	}

	callCtx, cancel := context.WithCancel(ctx)
	call := &whatsappCRMCall{
		id:          fmt.Sprintf("wa-call-%d", time.Now().UnixNano()),
		phoneID:     input.WhatsAppPhoneID,
		to:          input.PhoneNumber,
		signaling:   s.signaling,
		registry:    s.registry,
		publicIP:    s.publicMediaIP,
		stunServers: s.stunServers,
		events:      make(chan conversation_domain.CallEvent, 10),
		audioIn:     make(chan []byte, 200),
		done:        make(chan struct{}),
		ctx:         callCtx,
		cancel:      cancel,
		log:         s.log,
	}
	go call.dialAndBridge()
	return call, nil
}

type whatsappCRMCall struct {
	id          string
	phoneID     string
	to          string
	callID      string
	publicIP    string
	stunServers []string
	signaling   conversation_domain.WhatsAppCallSignaling
	registry    conversation_domain.WhatsAppCallRegistry

	media *media.Session

	events  chan conversation_domain.CallEvent
	audioIn chan []byte
	done    chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
	log    *log.Logger

	answeredOnce sync.Once
	hangupOnce   sync.Once
}

func (c *whatsappCRMCall) ID() string                                   { return c.id }
func (c *whatsappCRMCall) AudioStream() <-chan []byte                   { return c.audioIn }
func (c *whatsappCRMCall) Events() <-chan conversation_domain.CallEvent { return c.events }
func (c *whatsappCRMCall) Done() <-chan struct{}                        { return c.done }

func (c *whatsappCRMCall) SendAudio(pcm16 []byte) error {
	if c.media == nil {
		return nil
	}
	return c.media.WritePCM(pcm16)
}

func (c *whatsappCRMCall) Hangup() error {
	var err error
	c.hangupOnce.Do(func() {
		c.cancel()
		if strings.TrimSpace(c.callID) != "" {
			err = c.signaling.TerminateCall(context.Background(), c.phoneID, c.callID)
		}
		if c.media != nil {
			_ = c.media.Close()
		}
	})
	return err
}

func (c *whatsappCRMCall) dialAndBridge() {
	defer func() {
		close(c.done)
		time.Sleep(50 * time.Millisecond)
		close(c.events)
		close(c.audioIn)
	}()

	c.events <- conversation_domain.CallEvent{Type: conversation_domain.CallEventRinging}

	mediaSess, err := media.NewSession(c.publicIP, c.stunServers)
	if err != nil {
		c.log.Printf("[WACall] %s media setup failed: %v", c.id, err)
		c.events <- conversation_domain.CallEvent{Type: conversation_domain.CallEventFailed, Reason: err.Error()}
		return
	}
	c.media = mediaSess
	defer mediaSess.Close()

	callID, err := c.signaling.InitiateCall(c.ctx, conversation_domain.WhatsAppInitiateCallInput{
		PhoneID:       c.phoneID,
		ToPhoneNumber: c.to,
		SDPOffer:      mediaSess.Offer(),
		CallbackData:  c.id,
	})
	if err != nil {
		c.log.Printf("[WACall] %s initiate failed: %v", c.id, err)
		c.events <- conversation_domain.CallEvent{Type: classifyInitiateError(err), Reason: err.Error()}
		return
	}
	c.callID = callID

	signals := c.registry.Register(callID)
	defer c.registry.Unregister(callID)

	c.runSignalLoop(signals)
}

func (c *whatsappCRMCall) runSignalLoop(signals <-chan conversation_domain.WhatsAppCallSignal) {
	for {
		select {
		case <-c.ctx.Done():
			c.events <- conversation_domain.CallEvent{Type: conversation_domain.CallEventEnded}
			return
		case sig, ok := <-signals:
			if !ok {
				c.events <- conversation_domain.CallEvent{Type: conversation_domain.CallEventEnded}
				return
			}
			switch sig.Kind {
			case conversation_domain.WhatsAppCallConnect:
				if err := c.media.Start(sig.SDPAnswer); err != nil {
					c.log.Printf("[WACall] %s media start failed: %v", c.id, err)
					c.events <- conversation_domain.CallEvent{Type: conversation_domain.CallEventFailed, Reason: err.Error()}
					return
				}
				go c.pipeIncomingAudio()
			case conversation_domain.WhatsAppCallRinging:

			case conversation_domain.WhatsAppCallAccepted:
				c.answeredOnce.Do(func() {
					c.events <- conversation_domain.CallEvent{Type: conversation_domain.CallEventAnswered}
				})
			case conversation_domain.WhatsAppCallRejected:
				c.events <- conversation_domain.CallEvent{Type: conversation_domain.CallEventDeclined, Reason: sig.Reason}
				return
			case conversation_domain.WhatsAppCallTerminate:
				c.events <- conversation_domain.CallEvent{Type: conversation_domain.CallEventEnded, Reason: sig.Reason}
				return
			}
		}
	}
}

func (c *whatsappCRMCall) pipeIncomingAudio() {
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

func classifyInitiateError(err error) conversation_domain.CallEventType {
	if errors.Is(err, conversation_domain.ErrWhatsAppCallNoPermission) {
		return conversation_domain.CallEventDeclined
	}
	return conversation_domain.CallEventFailed
}
