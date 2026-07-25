package conversation_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"
	"github.com/zaf/g711"

	conversation_domain "vozko/domain/conversation"
	dialer_domain "vozko/domain/dialer"
	"vozko/domain/sip_trunk"
	"vozko/domain/voip"
	"vozko/infra/voip/audio"
)

const (
	trunkSampleRate   = 8000
	trunkFrameSamples = 160
	trunkFrameBytes   = 160
)

type rtpFramePacer interface {
	Wait(ctx context.Context, samples int) error
}

type trunkRTPPacer struct {
	clockRate int
	started   bool
	nextSend  time.Time
	timer     *time.Timer
}

func newTrunkRTPPacer(clockRate int) *trunkRTPPacer {
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	return &trunkRTPPacer{
		clockRate: clockRate,
		timer:     timer,
	}
}

func (p *trunkRTPPacer) Wait(ctx context.Context, samples int) error {
	if p == nil || p.clockRate <= 0 || samples <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	duration := time.Duration(samples) * time.Second / time.Duration(p.clockRate)
	if duration <= 0 {
		return nil
	}

	now := time.Now()
	if !p.started {
		p.started = true
		p.nextSend = now.Add(duration)
		return nil
	}

	waitTime := time.Until(p.nextSend)
	p.nextSend = p.nextSend.Add(duration)
	if waitTime <= 0 {
		if -waitTime > 200*time.Millisecond {
			p.nextSend = now.Add(duration)
		}
		return nil
	}

	if !p.timer.Stop() {
		select {
		case <-p.timer.C:
		default:
		}
	}
	p.timer.Reset(waitTime)

	select {
	case <-ctx.Done():
		p.timer.Stop()
		return ctx.Err()
	case <-p.timer.C:
		return nil
	}
}

type SIPTrunkCallSource struct {
	manager     sip_trunk.TrunkManager
	trunkRepo   sip_trunk.Repository
	getSIPTrunk sip_trunk.GetUseCase
	ownership   sip_trunk.OwnershipLookup
	log         *log.Logger
}

func NewSIPTrunkCallSource(
	manager sip_trunk.TrunkManager,
	trunkRepo sip_trunk.Repository,
	getSIPTrunk sip_trunk.GetUseCase,
	ownership sip_trunk.OwnershipLookup,
	logger *log.Logger,
) *SIPTrunkCallSource {
	if logger == nil {
		logger = log.Default()
	}
	return &SIPTrunkCallSource{
		manager:     manager,
		trunkRepo:   trunkRepo,
		getSIPTrunk: getSIPTrunk,
		ownership:   ownership,
		log:         logger,
	}
}

func (s *SIPTrunkCallSource) Name() string { return "sip_trunk" }

func (s *SIPTrunkCallSource) maybeTrunkBusy(trunkID string) *dialer_domain.ErrTrunkBusy {
	if s.ownership == nil {
		return nil
	}
	if s.ownership.IsOwner(trunkID) {
		return nil
	}
	return &dialer_domain.ErrTrunkBusy{
		TrunkID:    trunkID,
		Reason:     dialer_domain.TrunkBusyReasonReconciling,
		RetryAfter: 3 * time.Second,
	}
}

func (s *SIPTrunkCallSource) Dial(ctx context.Context, input conversation_domain.CallDialInput) (conversation_domain.CRMCall, error) {
	if s == nil || s.manager == nil {
		return nil, errors.New("sip trunk manager not configured")
	}
	if strings.TrimSpace(input.PhoneNumber) == "" {
		return nil, errors.New("phone number is required")
	}

	trunkID, err := s.resolveAndValidateTrunk(input)
	if err != nil {
		fmt.Println(" ERRO CARAIO: ", err)
		return nil, err
	}

	if busy := s.maybeTrunkBusy(trunkID); busy != nil {
		return nil, busy
	}

	callCtx, cancel := context.WithCancel(ctx)

	call := &sipTrunkCRMCall{
		id:          fmt.Sprintf("crm-call-%d", time.Now().UnixNano()),
		workspaceID: input.WorkspaceID,
		manager:     s.manager,
		phoneNumber: input.PhoneNumber,
		trunkID:     trunkID,
		events:      make(chan conversation_domain.CallEvent, 10),
		audioIn:     make(chan []byte, 200),
		done:        make(chan struct{}),
		readStopped: make(chan struct{}),
		ctx:         callCtx,
		cancel:      cancel,
		log:         s.log,
	}

	go call.dialAndBridge()

	return call, nil
}

func (s *SIPTrunkCallSource) resolveAndValidateTrunk(input conversation_domain.CallDialInput) (string, error) {
	entryType := strings.ToLower(strings.TrimSpace(input.EntryType))
	trunkID := strings.TrimSpace(input.SIPTrunkID)

	switch entryType {
	case "", "sip":
		if trunkID == "" {
			var err error
			trunkID, err = s.resolveWorkspaceFallbackTrunk(input.WorkspaceID)
			if err != nil {
				return "", err
			}
		}
	case "whatsapp":
		if trunkID == "" {
			return "", errors.New("sip trunk id is required for whatsapp entries")
		}
	default:
		if trunkID != "" {

			break
		}
		return "", fmt.Errorf("unsupported entry type: %s", entryType)
	}

	if s.getSIPTrunk != nil {
		trunk, err := s.getSIPTrunk.Execute(trunkID)
		if err != nil {
			return "", fmt.Errorf("sip trunk not found: %w", err)
		}
		if !input.IsAdmin {
			if !trunk.IsOwnedBy(input.WorkspaceID) && !trunk.IsGloballyVisible {
				return "", errors.New("you do not have access to the selected sip trunk")
			}
		}
	}

	return trunkID, nil
}

func (s *SIPTrunkCallSource) resolveWorkspaceFallbackTrunk(workspaceID string) (string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return "", errors.New("workspace id is required for direct dial without sip trunk id")
	}
	if s.trunkRepo == nil {
		return "", errors.New("sip trunk id is required for direct dial calls")
	}

	trunks, _, err := s.trunkRepo.FindAccessible(workspaceID, nil, 1, 1)
	if err != nil {
		return "", fmt.Errorf("load workspace sip trunks: %w", err)
	}
	if len(trunks) == 0 || trunks[0] == nil {
		return "", errors.New("workspace has no sip trunk available")
	}

	return strings.TrimSpace(trunks[0].ID), nil
}

type sipTrunkCRMCall struct {
	id          string
	workspaceID string
	manager     sip_trunk.TrunkManager
	phoneNumber string
	trunkID     string
	callID      string
	session     sip_trunk.TrunkCallSession

	events  chan conversation_domain.CallEvent
	audioIn chan []byte
	done    chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
	log    *log.Logger

	writeMu    sync.Mutex
	mediaReady bool
	seqNum     uint16
	timestamp  uint32
	ssrc       uint32
	writePacer rtpFramePacer

	pendingEncoded []byte
	codec          audio.Codec // negotiated G.711 law; nil => µ-law. Guarded by writeMu.

	hangupOnce sync.Once

	// surrender hands the raw media session to an external RTP bridge (a blind
	// transfer to a branch). Once surrendered, this call stops reading/writing the
	// session itself and parks the dialog alive until Hangup; the bridge is the sole
	// reader/writer. Mirrors the AI internalBridgeSurrender handoff.
	surrendered     atomic.Bool
	readStopped     chan struct{}
	readStoppedOnce sync.Once
}

func (c *sipTrunkCRMCall) markReadStopped() {
	c.readStoppedOnce.Do(func() { close(c.readStopped) })
}

// outCodec returns the negotiated outbound codec, defaulting to µ-law. Callers
// must hold writeMu.
func (c *sipTrunkCRMCall) outCodec() audio.Codec {
	if c.codec != nil {
		return c.codec
	}
	return audio.CodecMulaw
}

const sipTrunkMaxPendingEncoded = 16000

func (c *sipTrunkCRMCall) ID() string                                   { return c.id }
func (c *sipTrunkCRMCall) AudioStream() <-chan []byte                   { return c.audioIn }
func (c *sipTrunkCRMCall) Events() <-chan conversation_domain.CallEvent { return c.events }
func (c *sipTrunkCRMCall) Done() <-chan struct{}                        { return c.done }

func (c *sipTrunkCRMCall) SendAudio(pcm16 []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if !c.mediaReady || c.session.MediaSession == nil {
		return nil
	}

	codec := c.outCodec()
	encoded := codec.Encode(pcm16)
	if len(encoded) == 0 && len(c.pendingEncoded) == 0 {
		return nil
	}

	if len(c.pendingEncoded) > 0 {
		combined := make([]byte, 0, len(c.pendingEncoded)+len(encoded))
		combined = append(combined, c.pendingEncoded...)
		combined = append(combined, encoded...)
		encoded = combined
		c.pendingEncoded = c.pendingEncoded[:0]
	}

	offset := 0
	for ; offset+trunkFrameBytes <= len(encoded); offset += trunkFrameBytes {
		frame := encoded[offset : offset+trunkFrameBytes]

		if c.writePacer == nil {
			c.writePacer = newTrunkRTPPacer(trunkSampleRate)
		}
		if err := c.writePacer.Wait(c.ctx, trunkFrameSamples); err != nil {
			c.savePending(encoded[offset:])
			return fmt.Errorf("pace RTP: %w", err)
		}

		packet := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    codec.PayloadType(),
				SequenceNumber: c.seqNum,
				Timestamp:      c.timestamp,
				SSRC:           c.ssrc,
			},
			Payload: frame,
		}

		if err := c.session.MediaSession.WriteRTP(packet); err != nil {

			c.savePending(encoded[offset:])
			return fmt.Errorf("write RTP: %w", err)
		}

		c.seqNum++
		c.timestamp += trunkFrameSamples
	}

	if offset < len(encoded) {
		c.savePending(encoded[offset:])
	}

	return nil
}

func (c *sipTrunkCRMCall) savePending(tail []byte) {
	if len(tail) > sipTrunkMaxPendingEncoded {
		tail = tail[len(tail)-sipTrunkMaxPendingEncoded:]
	}
	c.pendingEncoded = append(c.pendingEncoded[:0], tail...)
}

func (c *sipTrunkCRMCall) Hangup() error {
	var hangupErr error
	c.hangupOnce.Do(func() {
		c.cancel()
		if c.trunkID != "" && c.callID != "" {
			hangupErr = c.manager.Hangup(context.Background(), c.trunkID, c.callID)
		}

		if c.session.MediaSession != nil {
			c.writeMu.Lock()
			c.mediaReady = false
			c.writeMu.Unlock()
			_ = c.session.MediaSession.Close()
		}
	})
	return hangupErr
}

func (c *sipTrunkCRMCall) dialAndBridge() {
	defer func() {
		close(c.done)
		time.Sleep(50 * time.Millisecond)
		close(c.events)
		close(c.audioIn)
	}()

	c.events <- conversation_domain.CallEvent{Type: conversation_domain.CallEventRinging}
	c.log.Printf("[CRMCall] %s dialing %s via SIP trunk %s...", c.id, c.phoneNumber, c.trunkID)

	session, err := c.manager.Invite(c.ctx, c.trunkID, sip_trunk.TrunkInviteInput{
		PhoneNumber: c.phoneNumber,
		Recording: &voip.RecordingMeta{
			CallID:      c.id,
			WorkspaceID: c.workspaceID,
		},
	})
	if err != nil {
		evType := classifyTrunkDialError(err)
		c.log.Printf("[CRMCall] %s dial failed on trunk %s: %v (classified as %s)", c.id, c.trunkID, err, evType)
		c.events <- conversation_domain.CallEvent{Type: evType, Reason: err.Error()}
		return
	}

	c.callID = session.ID
	c.session = session
	c.ssrc = uint32(time.Now().UnixNano() & 0xFFFFFFFF)

	if c.session.MediaSession == nil {
		c.log.Printf("[CRMCall] %s call established without media session", c.id)
		c.events <- conversation_domain.CallEvent{Type: conversation_domain.CallEventFailed, Reason: "no media session available"}
		return
	}

	c.log.Printf("[CRMCall] %s answered on trunk %s! Starting audio bridge for %s (codec=%s)",
		c.id, c.trunkID, c.phoneNumber, session.Media.SelectedCodec.Name)

	c.session.MediaSession.OnDTMF(func(digit rune) {
		c.log.Printf("[CRMCall] %s DTMF digit received: %c", c.id, digit)
	})

	c.sendNATHolePunch()
	// The codec is decided by the SIP trunk and carried on the media session;
	// read it from there (uniform across all channels) under writeMu, atomically
	// with mediaReady, so SendAudio always sees a consistent (codec, ready) pair.
	// See docs/SIP_AUDIO_PIPELINE.md §10 (P1).
	c.writeMu.Lock()
	c.codec = audio.G711CodecForName(c.session.MediaSession.NegotiatedCodec().Name)
	c.mediaReady = true
	c.writeMu.Unlock()
	c.events <- conversation_domain.CallEvent{Type: conversation_domain.CallEventAnswered}
	c.readSIPAudio()

	// If the media was surrendered to an external RTP bridge (blind transfer to a
	// branch), the dialog and session must stay alive until the bridge ends the call
	// via Hangup (which cancels ctx). Park here instead of tearing the call down, so
	// done/audioIn/events stay open and the caller keeps talking to the phone.
	if c.surrendered.Load() {
		c.log.Printf("[CRMCall] %s media surrendered to an external bridge; keeping the dialog up until Hangup", c.id)
		<-c.ctx.Done()
	}

	c.log.Printf("[CRMCall] %s audio bridge ended for %s", c.id, c.phoneNumber)
	c.events <- conversation_domain.CallEvent{Type: conversation_domain.CallEventEnded}
}

// SurrenderMedia stops this call's own RTP read/write loops and hands the raw media
// session to an external bridge, keeping the SIP dialog up. After it returns the
// bridge is the sole reader/writer of the session; Hangup still tears everything
// down. It mirrors the AI internalBridgeSurrender handoff and is idempotent.
func (c *sipTrunkCRMCall) SurrenderMedia() (voip.MediaSession, error) {
	if !c.surrendered.CompareAndSwap(false, true) {
		return nil, errors.New("sip trunk call: media already surrendered")
	}
	if c.session.MediaSession == nil {
		return nil, errors.New("sip trunk call: no media session to surrender")
	}

	// Stop our writer: SendAudio checks mediaReady, so clearing it makes every
	// subsequent SendAudio a no-op without racing the read side.
	c.writeMu.Lock()
	c.mediaReady = false
	c.writeMu.Unlock()

	// Wake the blocked reader so readSIPAudio observes the surrender and exits; the
	// one-shot UnblockReaders (past deadline -> drain -> deadline cleared) leaves the
	// session readable for the bridge. Wait for the reader to actually stop so the
	// bridge is never a second concurrent reader.
	if err := c.session.MediaSession.UnblockReaders(); err != nil {
		c.log.Printf("[CRMCall] %s surrender: UnblockReaders failed (proceeding): %v", c.id, err)
	}
	select {
	case <-c.readStopped:
	case <-time.After(2 * time.Second):
		c.log.Printf("[CRMCall] %s surrender: timed out waiting for the read loop to stop", c.id)
	}

	return c.session.MediaSession, nil
}

func (c *sipTrunkCRMCall) sendNATHolePunch() {
	if c.session.MediaSession == nil {
		return
	}
	c.log.Printf("[CRMCall] %s sending NAT hole-punch packets to %v", c.id, c.session.MediaSession.RemoteAddr())
	silence := make([]byte, trunkFrameBytes)
	for i := range silence {
		silence[i] = 0xFF
	}
	for i := 0; i < 5; i++ {
		pkt := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				Marker:         i == 0,
				PayloadType:    0,
				SequenceNumber: c.seqNum,
				Timestamp:      c.timestamp,
				SSRC:           c.ssrc,
			},
			Payload: append([]byte(nil), silence...),
		}
		if err := c.session.MediaSession.WriteRTP(pkt); err != nil {
			c.log.Printf("[CRMCall] %s NAT hole-punch WriteRTP error: %v", c.id, err)
			return
		}
		c.seqNum++
		c.timestamp += trunkFrameSamples
		time.Sleep(20 * time.Millisecond)
	}
	c.log.Printf("[CRMCall] %s NAT hole-punch complete", c.id)
}

func (c *sipTrunkCRMCall) readSIPAudio() {
	defer c.markReadStopped()
	buf := make([]byte, 1500)

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		// Stop deterministically on surrender BEFORE the next read. UnblockReaders is
		// absorbed by the reorder buffer (it keeps the buffer alive across the transient
		// timeout), so a blocked ReadRTP is woken but keeps returning packets; checking
		// the flag at the top of the loop means the external RTP bridge becomes the sole
		// reader within one packet instead of waiting out the surrender timeout.
		if c.surrendered.Load() {
			return
		}

		packet := &rtp.Packet{}
		if _, err := c.session.MediaSession.ReadRTP(buf, packet); err != nil {
			// Surrender wakes this read (UnblockReaders) on purpose so the external
			// RTP bridge can take over the session; that is not an error.
			if c.surrendered.Load() {
				return
			}
			if c.ctx.Err() != nil {
				return
			}
			c.log.Printf("[CRMCall] %s ReadRTP error: %v", c.id, err)
			return
		}

		if len(packet.Payload) == 0 {
			continue
		}

		var pcm16 []byte
		switch packet.PayloadType {
		case 0:
			pcm16 = g711.DecodeUlaw(packet.Payload)
		case 8:
			pcm16 = g711.DecodeAlaw(packet.Payload)
		default:
			continue
		}

		select {
		case c.audioIn <- pcm16:
		default:
		}
	}
}

func classifyTrunkDialError(err error) conversation_domain.CallEventType {
	if err == nil {
		return conversation_domain.CallEventFailed
	}

	errStr := strings.ToLower(err.Error())
	switch {
	case strings.Contains(errStr, "486") || strings.Contains(errStr, "busy"):
		return conversation_domain.CallEventBusy
	case strings.Contains(errStr, "480") || strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline") || strings.Contains(errStr, "no answer"):
		return conversation_domain.CallEventNoAnswer
	case strings.Contains(errStr, "603") || strings.Contains(errStr, "decline") || strings.Contains(errStr, "403") || strings.Contains(errStr, "forbidden"):
		return conversation_domain.CallEventDeclined
	default:
		return conversation_domain.CallEventFailed
	}
}
