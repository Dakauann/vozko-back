package voipinfra

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/pion/rtp"

	"vozko/domain/branch"
	"vozko/domain/voip"
	"vozko/infra/voip/audio"
)

var errNilConsultPeer = errors.New("branch consult: nil consult peer")

func errNoAnsweredDialog(branchID string) error {
	return fmt.Errorf("branch %s: no answered dialog to open a consult", branchID)
}

// branchConsultRelay is the media half of an ATTENDED transfer to a SIP branch:
// it transcodes between the answered phone's G.711 RTP leg and a PCM consult peer
// (the initiating agent's browser, via the consult bridge). It is the mirror of
// RTPBridge, except one side is PCM (the browser) instead of a second RTP leg, so
// it decodes inbound G.711 -> PCM16 for the agent and re-originates paced G.711
// RTP from the agent's PCM using the mature G711RTPStream. The phone leg's
// lifecycle is owned by the registrar (parked), NOT by this relay: stop() ends
// the transcode without hanging up, so an attended completion can reuse the leg.
type branchConsultRelay struct {
	phone  voip.MediaSession
	peer   branch.ConsultPeer
	codec  audio.Codec
	stream *audio.G711RTPStream
	logger *log.Logger

	cancel   context.CancelFunc
	wg       sync.WaitGroup
	stopOnce sync.Once

	// onPhoneGone fires once if the phone hangs up during the consult (as opposed
	// to a deliberate stop()), so the transfer aborts as a target-disconnect.
	onPhoneGone func()
	goneOnce    sync.Once
}

func newBranchConsultRelay(phone voip.MediaSession, peer branch.ConsultPeer, onPhoneGone func(), logger *log.Logger) *branchConsultRelay {
	if logger == nil {
		logger = log.Default()
	}
	codec := audio.CodecMulaw
	if c, ok := audio.CodecForName(phone.NegotiatedCodec().Name); ok {
		codec = c
	}
	stream := audio.New(phone, audio.Options{Codec: codec})
	return &branchConsultRelay{
		phone:       phone,
		peer:        peer,
		codec:       codec,
		stream:      stream,
		logger:      logger,
		onPhoneGone: onPhoneGone,
	}
}

func (r *branchConsultRelay) start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	// peer -> phone: the G711RTPStream paces the agent's PCM as 20ms G.711 frames.
	r.stream.Run(ctx)

	r.wg.Add(2)
	// phone -> peer: decode inbound G.711 to PCM16 for the agent's browser.
	go func() {
		defer r.wg.Done()
		buf := make([]byte, 2048)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			packet := &rtp.Packet{}
			if _, err := r.phone.ReadRTP(buf, packet); err != nil {
				if ctx.Err() != nil {
					return // deliberate stop
				}
				r.logger.Printf("[branch-consult] phone read ended: %v", err)
				cancel()
				r.firePhoneGone()
				return
			}
			if len(packet.Payload) == 0 {
				continue // comfort noise / keep-alive
			}
			codec := r.codec
			if c, ok := audio.CodecForPayloadType(packet.PayloadType); ok {
				codec = c
			}
			if pcm := codec.Decode(packet.Payload); len(pcm) > 0 {
				r.peer.Send(pcm)
			}
		}
	}()
	// agent -> phone: drain the consult peer's PCM into the paced RTP stream.
	go func() {
		defer r.wg.Done()
		recv := r.peer.Recv()
		for {
			select {
			case <-ctx.Done():
				return
			case pcm, ok := <-recv:
				if !ok {
					return // the consult bridge closed
				}
				if len(pcm) > 0 {
					_ = r.stream.WritePCM16(ctx, pcm)
				}
			}
		}
	}()
}

func (r *branchConsultRelay) firePhoneGone() {
	r.goneOnce.Do(func() {
		if r.onPhoneGone == nil {
			return
		}
		// Dispatch ASYNC: onPhoneGone drives the transfer abort, which ends in
		// DetachConsult -> stop() -> wg.Wait(). Calling it inline would run that on
		// THIS relay goroutine, so wg.Wait() would wait for a goroutine that is
		// blocked in the callback — a self-join deadlock that leaves the branch
		// reserved (member stuck "on call") until the reservation TTL. Running it on
		// its own goroutine lets this one return and Done() so stop() completes.
		go r.onPhoneGone()
	})
}

// stop ends the transcode relay synchronously (both goroutines joined) WITHOUT
// hanging up the phone: an attended completion reuses the parked leg for the
// caller bridge. Idempotent.
func (r *branchConsultRelay) stop() {
	r.stopOnce.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}
		r.stream.Stop()
		// A blocking ReadRTP is not interrupted by context alone; wake it. This is
		// a one-shot deadline wake (see the surrender/handoff path), so a later
		// StartBridge on the same phone leg reads normally.
		_ = r.phone.UnblockReaders()
	})
	r.wg.Wait()
}

// --- registrar wiring (branch.BranchMediaBridge consult half) ------------

// StartConsultBridge peeks the parked answered phone leg (leaving it parked so an
// attended completion can StartBridge it to the caller) and starts the transcode
// relay to the PCM consult peer.
func (r *BranchRegistrar) StartConsultBridge(branchID string, peer branch.ConsultPeer, onPhoneGone func()) (func(), error) {
	if peer == nil {
		return nil, errNilConsultPeer
	}
	r.answeredMu.Lock()
	a := r.answered[branchID]
	r.answeredMu.Unlock()
	if a == nil {
		return nil, errNoAnsweredDialog(branchID)
	}

	relay := newBranchConsultRelay(a.media, peer, onPhoneGone, r.logger)
	relay.start(r.ctx)
	r.logger.Printf("[branch-consult] branch %s: consult relay up (agent <-> phone)", branchID)
	return relay.stop, nil
}

// CancelConsult hangs up the parked phone leg after a cancelled consult. It is a
// no-op if the leg was already consumed by StartBridge (completion).
func (r *BranchRegistrar) CancelConsult(branchID string) {
	if a := r.clearAnswered(branchID); a != nil {
		r.logger.Printf("[branch-consult] branch %s: consult cancelled, hanging up the phone", branchID)
		a.hangup()
	}
}
