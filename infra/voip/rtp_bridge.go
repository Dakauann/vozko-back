package voipinfra

import (
	"context"
	"log"
	"sync"

	"github.com/pion/rtp"

	"vozko/domain/voip"
)

// RTPBridge is a bidirectional RTP relay between two media legs. It is the media
// half of a blind transfer to a SIP branch (branch): leg A is the answered phone
// (the branch dialog's MediaSession) and leg B is the party being transferred (the
// carrier leg's MediaSession, surrendered so this relay is its sole reader).
//
// It is the FIRST leg-to-leg relay in the codebase: every prior "bridge" terminates
// RTP into PCM for a WebSocket, STT or TTS, so there is nothing to reuse and this is
// self-contained and unit-tested against fake sessions. It does NOT transcode: a
// blind transfer to a branch negotiates the same G.711 flavour on both legs (Brazil
// is A-law end to end), so a normal SIP-sized packet is forwarded as-is. It DOES
// re-packetize a payload too large to relay (the agent simulator's channel-based
// media emits ~2048-byte frames that overflow the 1500-byte RTP write buffer) into
// standard 20 ms G.711 frames, same codec, so this is framing, not transcoding. If
// the two legs ever negotiate different payload types a transcode step belongs here,
// guarded on NegotiatedCodec().
//
// LIVE-SIP NOTE: passthrough preserves the source stream's SSRC/sequence/timestamp.
// This is the one detail that must be confirmed against a real phone: a receiver
// with a strict jitter buffer may require the relay to rewrite the SSRC to its own.
// Everything else (lifecycle, teardown, reader unblocking) is exercised in tests.
type RTPBridge struct {
	a, b   voip.MediaSession
	logger *log.Logger

	cancel context.CancelFunc
	wg     sync.WaitGroup
	stop   sync.Once
	done   chan struct{}
}

// NewRTPBridge pairs two media legs. Start begins relaying; Stop tears it down.
func NewRTPBridge(a, b voip.MediaSession, logger *log.Logger) *RTPBridge {
	if logger == nil {
		logger = log.Default()
	}
	return &RTPBridge{a: a, b: b, logger: logger, done: make(chan struct{})}
}

// Start launches the two relay goroutines. It is non-blocking; the relay runs until
// Stop is called, the parent context is cancelled, or either leg's reader errors
// (typically the far end hanging up). Done closes once both pumps have exited.
func (br *RTPBridge) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	br.cancel = cancel
	br.wg.Add(2)
	go br.pump(ctx, br.a, br.b, "a->b")
	go br.pump(ctx, br.b, br.a, "b->a")
	go func() {
		br.wg.Wait()
		close(br.done)
	}()
}

// Done closes when the relay has fully stopped (either leg dropped, or Stop was
// called and both pumps exited). A caller uses it to react to the phone hanging up.
func (br *RTPBridge) Done() <-chan struct{} { return br.done }

const (
	// rtpReadBufSize is the per-pump read buffer. 2048 lets the pump receive the
	// oversized frames the agent simulator emits (~2048-byte PCMU payloads from its
	// channel-based virtual media session); a real SIP leg's frames are ~172 bytes and
	// fit trivially.
	rtpReadBufSize = 2048
	// maxRelayPayload is the largest payload forwarded as-is. It sits under diago's
	// 1500-byte RTP write buffer (media.RTPBufSize) with headroom for the header and any
	// SRTP tag, so every normal SIP ptime (20/30/40 ms G.711 = 160/240/320 B) passes
	// through untouched. A larger payload is re-framed below (see relay).
	maxRelayPayload = 1400
	// g711FrameBytes is one 20 ms G.711 frame (8 kHz, 1 byte/sample), the ptime a real
	// phone's jitter buffer expects when an oversized source packet is re-framed.
	g711FrameBytes = 160
)

// reframeState carries the re-originated sequence/timestamp for one relay direction,
// used only when an oversized source packet has to be split into standard frames.
type reframeState struct {
	started bool
	seq     uint16
	ts      uint32
}

func (br *RTPBridge) pump(ctx context.Context, src, dst voip.MediaSession, dir string) {
	defer br.wg.Done()
	// One reusable payload buffer + packet per pump, exactly like readSIPAudio.
	buf := make([]byte, rtpReadBufSize)
	var out reframeState
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		packet := &rtp.Packet{}
		if _, err := src.ReadRTP(buf, packet); err != nil {
			// A cancelled bridge unblocks the reader (Stop -> UnblockReaders), so a
			// read error during teardown is expected and silent.
			if ctx.Err() != nil {
				return
			}
			br.logger.Printf("[rtp-bridge] %s read ended: %v", dir, err)
			br.cancel() // one leg dropping ends the whole bridge
			return
		}

		if len(packet.Payload) == 0 {
			continue // comfort noise / keep-alive with no audio; nothing to relay
		}

		if err := br.relay(dst, packet, &out); err != nil {
			if ctx.Err() != nil {
				return
			}
			br.logger.Printf("[rtp-bridge] %s write ended: %v", dir, err)
			br.cancel()
			return
		}
	}
}

// relay forwards one packet to dst. A normal SIP-sized payload is forwarded as-is
// (preserving the source SSRC/sequence/timestamp). An oversized payload, the agent
// simulator emits ~2048-byte PCMU frames from its channel-based media, which overflow
// the receiver's RTP write buffer, is split into 20 ms G.711 frames with a
// re-originated monotonic sequence/timestamp so a real phone accepts the stream. This
// path is never taken by a real SIP leg (its frames already fit maxRelayPayload).
func (br *RTPBridge) relay(dst voip.MediaSession, p *rtp.Packet, out *reframeState) error {
	if len(p.Payload) <= maxRelayPayload {
		return dst.WriteRTP(p)
	}
	if !out.started {
		out.started = true
		out.seq = p.SequenceNumber
		out.ts = p.Timestamp
	}
	for off := 0; off < len(p.Payload); off += g711FrameBytes {
		end := min(off+g711FrameBytes, len(p.Payload))
		frame := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    p.PayloadType,
				SSRC:           p.SSRC,
				SequenceNumber: out.seq,
				Timestamp:      out.ts,
			},
			Payload: p.Payload[off:end],
		}
		if err := dst.WriteRTP(frame); err != nil {
			return err
		}
		out.seq++
		out.ts += uint32(end - off)
	}
	return nil
}

// Stop cancels the relay and unblocks both readers so the pump goroutines observe
// the cancellation and exit, then waits for them. It is idempotent and safe to call
// from any goroutine. It does NOT close the underlying sessions: their lifecycle is
// owned by the call/dialog, not the bridge.
func (br *RTPBridge) Stop() {
	br.stop.Do(func() {
		if br.cancel != nil {
			br.cancel()
		}
		// A blocking ReadRTP is not interrupted by context alone; unblock it.
		_ = br.a.UnblockReaders()
		_ = br.b.UnblockReaders()
	})
	br.wg.Wait()
}
