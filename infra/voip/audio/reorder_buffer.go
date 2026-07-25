package audio

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pion/rtp"

	"vozko/domain/voip"
)

const (
	DefaultReorderDepth   = 3
	DefaultReorderMaxWait = 60 * time.Millisecond

	AlawSilence = byte(0xD5)

	initialFrameBytes = 160

	// DefaultPLCDecay is the per-frame attenuation applied to a repeated frame
	// during packet-loss concealment. Successive concealed frames compound it
	// (lastPayload is updated to each emitted frame), so a sustained gap fades
	// smoothly toward silence instead of repeating at full level (which sounds
	// buzzy/robotic). This applies the attenuate-toward-silence principle of
	// ITU-T G.711 Appendix I (the basis of Asterisk's plc.c) at frame
	// granularity; it intentionally does NOT do the pitch-period extraction of
	// the full Appendix I algorithm. See docs/SIP_AUDIO_PIPELINE.md §10 (P4).
	DefaultPLCDecay = 0.5

	// plcSilenceFloor is the peak PCM magnitude below which a faded concealment
	// frame is snapped to clean codec silence (≈ -54 dBFS).
	plcSilenceFloor = 64

	// maxConcealPackets bounds how many synthetic frames packet-loss concealment
	// will ever generate for a single gap (~1 s at 20 ms). Beyond this, the gap
	// is treated as a stream discontinuity (SSRC change / re-INVITE / huge loss)
	// and the buffer RESYNCs to live audio instead of synthesizing thousands of
	// frames. Without this cap a sequence-number/SSRC discontinuity makes the
	// fill loop run up to 65535 times, flooding and desyncing the downstream
	// pipeline (observed in production as "[reorder] gap timer: filled 65533").
	maxConcealPackets = 50

	// readTimeoutBackoff is the pause between retries while readFromInner is
	// absorbing transient read-deadline timeouts (see readFromInner).
	readTimeoutBackoff = 5 * time.Millisecond

	// maxConsecutiveReadTimeouts bounds how long readFromInner tolerates an
	// unbroken run of read-deadline timeouts before declaring the socket dead.
	// A transient timeout is how mediaSessionAdapter.UnblockReaders() momentarily
	// wakes this goroutine (it sets a read deadline in the past for ~50ms, then
	// clears it) so an in-flight AI→human surrender can re-home the live media —
	// so timeouts must NOT tear the buffer down. But a deadline that is never
	// cleared must not spin forever either; at readTimeoutBackoff per iteration
	// this caps the tolerated run at ~2s before the buffer gives up.
	maxConsecutiveReadTimeouts = 400
)

// closeDrainTimeout bounds how long Close() waits for mainLoop to finish draining
// and close doneCh. Close() MUST return even if the inner RTP socket is so wedged
// that neither UnblockReaders nor inner.Close() ever wakes the blocked reader: the
// call bridge releases the workspace call slot only AFTER Close() returns (via
// `defer releaseCallSlot`), so an unbounded wait here leaks the slot forever — the
// root cause of the campaign stalling at capacity with phantom slots while real
// calls sit far below the plan limit. A healthy drain is sub-millisecond; this cap
// only bites on a truly wedged socket. It is a var (not a const) so teardown tests
// can shorten it.
var closeDrainTimeout = 3 * time.Second

var ErrBufferClosed = fmt.Errorf("audio: reorder buffer closed")

// isReadTimeout reports whether err is a (transient) read-deadline timeout, as
// produced by mediaSessionAdapter.UnblockReaders setting a past read deadline.
func isReadTimeout(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return ne.Timeout()
	}
	return false
}

type ReorderBufferStats struct {
	ReorderedPackets   uint64
	LostPackets        uint64
	LateDroppedPackets uint64
	SilenceFilled      uint64
	// Resyncs counts stream discontinuities (SSRC change / oversized gap) where
	// the buffer skipped ahead to live audio instead of concealing the gap.
	Resyncs uint64
	// Received counts real RTP packets read from the inner session (the
	// denominator for a per-call loss rate). SilenceFilled is the count of
	// missing packets the buffer had to conceal, so the inbound loss rate is
	// SilenceFilled / (Received + SilenceFilled).
	Received uint64
}

type RTPReorderBufferOptions struct {
	Depth   int
	MaxWait time.Duration
	// PLCDecay is the per-frame attenuation for packet-loss concealment in
	// (0,1]. <=0 uses DefaultPLCDecay; >=1 means constant-level repeat (legacy
	// behaviour).
	PLCDecay float64
	Logger   func(format string, args ...interface{})
	// CallID labels the per-call [reorder-stats] summary emitted on Close so it
	// can be correlated to a specific call in the logs.
	CallID string
}

type storedPacket struct {
	header  rtp.Header
	payload []byte
}

type readResult struct {
	header  rtp.Header
	payload []byte
	n       int
	err     error
}

type deliverResult struct {
	header  rtp.Header
	payload []byte
	n       int
	err     error
}

type RTPReorderBuffer struct {
	inner voip.MediaSession
	opts  RTPReorderBufferOptions

	mu          sync.Mutex
	packets     map[uint16]*storedPacket
	nextSeq     uint16
	lastTS      uint32
	tsDelta     uint32
	ssrc        uint32
	started     bool
	observedPT  uint8
	frameBytes  int
	lastPayload []byte
	plcDecay    float64
	stats       ReorderBufferStats

	deliverCh    chan deliverResult
	readCh       chan readResult
	stopCh       chan struct{}
	doneCh       chan struct{}
	runOnce      sync.Once
	closeOnce    sync.Once
	logStatsOnce sync.Once
	ctx          context.Context
	cancel       context.CancelFunc
}

func NewRTPReorderBuffer(inner voip.MediaSession, opts RTPReorderBufferOptions) *RTPReorderBuffer {
	if opts.Depth <= 0 {
		opts.Depth = DefaultReorderDepth
	}
	if opts.MaxWait <= 0 {
		opts.MaxWait = DefaultReorderMaxWait
	}
	if opts.PLCDecay <= 0 {
		opts.PLCDecay = DefaultPLCDecay
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &RTPReorderBuffer{
		inner:      inner,
		opts:       opts,
		plcDecay:   opts.PLCDecay,
		frameBytes: initialFrameBytes,
		tsDelta:    uint32(initialFrameBytes),
		packets:    make(map[uint16]*storedPacket),
		deliverCh:  make(chan deliverResult, 8),
		readCh:     make(chan readResult, 1),
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (rb *RTPReorderBuffer) Run(ctx context.Context) {
	rb.runOnce.Do(func() {
		// The loops run on rb.ctx — the context Close() cancels via rb.cancel — and
		// the caller's ctx is bridged into it, so BOTH the call ending (ctx) and
		// Close() stop them. Previously the loops ran on the caller's ctx while
		// Close() cancelled an unrelated internal ctx (rb.ctx), so cancellation never
		// reached them: teardown then relied solely on inner.Close() unblocking the
		// RTP read, which deadlocks on a wedged/half-open socket. That was the ~19h
		// RTPReorderBuffer.Close hang seen in pprof — the bridge goroutine stuck in
		// Close(), so `defer releaseCallSlot` never ran and the slot leaked.
		go func() {
			select {
			case <-ctx.Done():
				rb.cancel()
			case <-rb.ctx.Done():
			}
		}()
		go rb.readFromInner(rb.ctx)
		go rb.mainLoop(rb.ctx)
	})
}

// logf emits a diagnostic line when a logger is configured; no-op otherwise.
func (rb *RTPReorderBuffer) logf(format string, args ...any) {
	if rb.opts.Logger != nil {
		rb.opts.Logger(format, args...)
	}
}

func (rb *RTPReorderBuffer) readFromInner(ctx context.Context) {
	buf := make([]byte, 1500)
	consecutiveTimeouts := 0
	for {
		select {
		case <-ctx.Done():
			rb.logf("[reorder] call=%s readFromInner stopping: context cancelled", rb.opts.CallID)
			return
		default:
		}

		pkt := &rtp.Packet{}
		n, err := rb.inner.ReadRTP(buf, pkt)

		if err != nil && isReadTimeout(err) {
			// A transient read-deadline timeout is NOT a stream failure: it is how
			// mediaSessionAdapter.UnblockReaders() momentarily wakes this goroutine
			// (it sets a read deadline in the past for ~50ms, then clears it) so an
			// in-flight AI→human surrender can re-home the live media onto the human
			// leg. Propagating it would tear the whole reorder buffer down — closing
			// the media out from under the call the human just accepted (the inbound
			// "audio: reorder buffer closed" call-drop bug). Keep reading; the
			// deadline is cleared again within ~50ms.
			consecutiveTimeouts++
			if consecutiveTimeouts == 1 {
				rb.logf("[reorder] call=%s absorbing transient read timeout (likely UnblockReaders surrender hand-off); keeping buffer alive", rb.opts.CallID)
			}
			if consecutiveTimeouts < maxConsecutiveReadTimeouts {
				select {
				case <-ctx.Done():
					return
				case <-time.After(readTimeoutBackoff):
				}
				continue
			}
			// The deadline was never cleared — treat the socket as genuinely dead
			// and fall through to propagate the error below.
			rb.logf("[reorder] call=%s read deadline stuck across %d timeouts (~%s); treating socket as dead",
				rb.opts.CallID, consecutiveTimeouts, time.Duration(consecutiveTimeouts)*readTimeoutBackoff)
		}
		consecutiveTimeouts = 0

		if err != nil {
			rb.logf("[reorder] call=%s inner read error: %v — closing buffer", rb.opts.CallID, err)
		}

		payload := make([]byte, len(pkt.Payload))
		copy(payload, pkt.Payload)
		result := readResult{
			header:  pkt.Header,
			payload: payload,
			n:       n,
			err:     err,
		}

		select {
		case rb.readCh <- result:
		case <-ctx.Done():
			return
		}

		if err != nil {
			return
		}
	}
}

func (rb *RTPReorderBuffer) mainLoop(ctx context.Context) {
	defer close(rb.deliverCh)
	defer close(rb.doneCh)

	var gapTimer *time.Timer
	var gapTimerCh <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			rb.logf("[reorder] call=%s mainLoop stopping: context cancelled", rb.opts.CallID)
			rb.drainRemaining()
			return
		case result, ok := <-rb.readCh:
			if !ok {
				rb.logf("[reorder] call=%s mainLoop stopping: read channel closed", rb.opts.CallID)
				return
			}
			if result.err != nil {
				rb.logf("[reorder] call=%s mainLoop stopping: propagating inner read error: %v", rb.opts.CallID, result.err)
				rb.sendDeliver(ctx, deliverResult{err: result.err})
				return
			}

			rb.mu.Lock()
			rb.stats.Received++
			if !rb.started {
				rb.nextSeq = result.header.SequenceNumber
				rb.lastTS = result.header.Timestamp
				rb.ssrc = result.header.SSRC
				rb.observedPT = result.header.PayloadType
				rb.started = true
				rb.frameBytes = len(result.payload)
				if rb.frameBytes <= 0 {
					rb.frameBytes = initialFrameBytes
				}
				rb.tsDelta = uint32(rb.frameBytes)
				rb.lastPayload = make([]byte, len(result.payload))
				copy(rb.lastPayload, result.payload)
				rb.packets[rb.nextSeq] = &storedPacket{header: result.header, payload: result.payload}
				rb.mu.Unlock()
			} else if result.header.SSRC != rb.ssrc {
				// The far end restarted the media stream (re-INVITE, codec
				// renegotiation, or a re-latch to a new source). Its sequence
				// numbers are unrelated to ours, so concealing the "gap" would
				// synthesize garbage. Resync to the new stream instead.
				oldSSRC := rb.ssrc
				rb.resyncLocked(result)
				rb.mu.Unlock()
				if rb.opts.Logger != nil {
					rb.opts.Logger("[reorder] SSRC change %d->%d: resynced to seq %d",
						oldSSRC, result.header.SSRC, result.header.SequenceNumber)
				}
			} else {
				seq := result.header.SequenceNumber
				diff := seqDiff(seq, rb.nextSeq)
				if diff < 0 {
					rb.stats.LateDroppedPackets++
					rb.mu.Unlock()
				} else if int(diff) > maxConcealPackets {
					// Gap too large to conceal (huge loss / discontinuity).
					// Skip ahead to live audio rather than synthesizing
					// thousands of frames (the 65533-fill bug).
					gap := int(diff)
					rb.resyncLocked(result)
					rb.mu.Unlock()
					if rb.opts.Logger != nil {
						rb.opts.Logger("[reorder] gap %d > %d: resynced ahead to seq %d (no mass conceal)",
							gap, maxConcealPackets, seq)
					}
				} else if diff > int16(rb.opts.Depth) {
					gap := int(diff)
					rb.mu.Unlock()
					if rb.opts.Logger != nil {
						rb.opts.Logger("[reorder] large gap: filling %d missing packets", gap)
					}
					for i := 0; i < gap; i++ {
						if !rb.fillSilenceAndDrain(ctx) {
							return
						}
					}
					rb.mu.Lock()
					rb.packets[seq] = &storedPacket{header: result.header, payload: result.payload}
					rb.mu.Unlock()
				} else {
					rb.packets[seq] = &storedPacket{header: result.header, payload: result.payload}
					rb.mu.Unlock()
				}
			}

			if !rb.drainToChannel(ctx) {
				return
			}

			rb.mu.Lock()
			hasGap := rb.hasGapLocked()
			rb.mu.Unlock()
			if hasGap && gapTimer == nil {
				gapTimer = time.NewTimer(rb.opts.MaxWait)
				gapTimerCh = gapTimer.C
			} else if !hasGap && gapTimer != nil {
				gapTimer.Stop()
				gapTimerCh = nil
			}

		case <-gapTimerCh:
			gapFilled := 0
			for gapFilled < maxConcealPackets {
				rb.mu.Lock()
				if !rb.hasGapLocked() {
					rb.mu.Unlock()
					break
				}
				rb.fillSilenceOneLocked()
				gapFilled++
				rb.mu.Unlock()
				if !rb.drainToChannel(ctx) {
					return
				}
			}
			// If a gap still remains after the conceal cap, the awaited packet is
			// too far ahead to be real loss — skip ahead to the earliest buffered
			// packet instead of looping toward a uint16 wraparound (65533 fills).
			rb.mu.Lock()
			if rb.hasGapLocked() {
				rb.skipToEarliestLocked()
			}
			rb.mu.Unlock()
			if !rb.drainToChannel(ctx) {
				return
			}
			if gapFilled > 0 && rb.opts.Logger != nil {
				rb.opts.Logger("[reorder] gap timer: filled %d missing packets", gapFilled)
			}
			rb.mu.Lock()
			hasGap := rb.hasGapLocked()
			rb.mu.Unlock()
			if hasGap {
				gapTimer.Reset(rb.opts.MaxWait)
			} else {
				gapTimer.Stop()
				gapTimerCh = nil
			}
		}
	}
}

func (rb *RTPReorderBuffer) fillSilenceAndDrain(ctx context.Context) bool {
	rb.mu.Lock()
	rb.fillSilenceOneLocked()
	rb.mu.Unlock()
	return rb.drainToChannel(ctx)
}

func (rb *RTPReorderBuffer) hasGapLocked() bool {
	if !rb.started {
		return false
	}
	_, exists := rb.packets[rb.nextSeq]
	return !exists && len(rb.packets) > 0
}

// resyncLocked discards any buffered (now-stale) packets and re-anchors the
// buffer on the given packet's stream — used when the SSRC changes or a gap is
// too large to conceal. It makes that packet the next one to deliver, so the
// timeline jumps to live audio instead of synthesizing the (possibly
// wrapped-around) gap.
func (rb *RTPReorderBuffer) resyncLocked(result readResult) {
	rb.packets = make(map[uint16]*storedPacket)
	rb.nextSeq = result.header.SequenceNumber
	rb.ssrc = result.header.SSRC
	rb.observedPT = result.header.PayloadType
	rb.lastTS = result.header.Timestamp
	if n := len(result.payload); n > 0 {
		rb.frameBytes = n
		rb.tsDelta = uint32(n)
		rb.lastPayload = append(rb.lastPayload[:0], result.payload...)
	}
	rb.packets[rb.nextSeq] = &storedPacket{header: result.header, payload: result.payload}
	rb.stats.Resyncs++
}

// skipToEarliestLocked advances nextSeq to the buffered packet closest ahead of
// the current position, so a stuck gap (awaited packet far ahead / wrapped) is
// skipped instead of concealed indefinitely. No-op when nothing is buffered.
func (rb *RTPReorderBuffer) skipToEarliestLocked() {
	if len(rb.packets) == 0 {
		return
	}
	var best uint16
	bestSet := false
	var bestDist int
	for seq := range rb.packets {
		dist := int(uint16(seq - rb.nextSeq)) // forward distance, wrap-aware
		if !bestSet || dist < bestDist {
			best, bestDist, bestSet = seq, dist, true
		}
	}
	rb.nextSeq = best
	rb.stats.Resyncs++
}

func (rb *RTPReorderBuffer) fillSilenceOneLocked() {
	payload := rb.concealPayloadLocked()

	ts := rb.lastTS + rb.tsDelta
	header := rtp.Header{
		Version:        2,
		PayloadType:    rb.observedPT,
		SequenceNumber: rb.nextSeq,
		Timestamp:      ts,
		SSRC:           rb.ssrc,
	}
	rb.packets[rb.nextSeq] = &storedPacket{header: header, payload: payload}
	rb.stats.SilenceFilled++
	rb.lastTS = ts
	rb.tsDelta = uint32(len(payload))
}

// concealPayloadLocked builds one packet-loss-concealment frame. For a known
// G.711 codec with a previous frame it repeats that frame attenuated by
// plcDecay; because lastPayload is updated to each emitted frame by
// drainToChannel, consecutive concealed frames compound the attenuation and the
// gap fades smoothly toward silence. Once faded below plcSilenceFloor — or when
// the codec/last frame is unknown — it emits clean codec silence. This is a
// simplified, frame-level approximation of the attenuate-toward-silence
// behaviour of ITU-T G.711 Appendix I (Asterisk's plc.c); it does not perform
// the pitch-period extraction of the full algorithm.
func (rb *RTPReorderBuffer) concealPayloadLocked() []byte {
	codec, ok := CodecForPayloadType(rb.observedPT)
	if ok {
		if len(rb.lastPayload) > 0 {
			pcm := codec.Decode(rb.lastPayload)
			if PeakAmplitude(pcm) > plcSilenceFloor {
				return codec.Encode(ApplyGainLimited(pcm, rb.plcDecay))
			}
			return codec.Silence(len(rb.lastPayload))
		}
		return codec.Silence(rb.frameLenLocked())
	}

	// Unknown/non-G.711 payload: best effort — repeat the last frame verbatim,
	// else fill µ-law idle (observedPT is unknown so we can't pick a law).
	if len(rb.lastPayload) > 0 {
		return append([]byte(nil), rb.lastPayload...)
	}
	out := make([]byte, rb.frameLenLocked())
	for i := range out {
		out[i] = MulawSilence
	}
	return out
}

func (rb *RTPReorderBuffer) frameLenLocked() int {
	if rb.frameBytes > 0 {
		return rb.frameBytes
	}
	return initialFrameBytes
}

func (rb *RTPReorderBuffer) drainToChannel(ctx context.Context) bool {
	for {
		rb.mu.Lock()
		sp, exists := rb.packets[rb.nextSeq]
		if !exists {
			rb.mu.Unlock()
			return true
		}
		delete(rb.packets, rb.nextSeq)
		rb.nextSeq++
		rb.lastTS = sp.header.Timestamp
		if len(sp.payload) > 0 {
			rb.frameBytes = len(sp.payload)
			rb.tsDelta = uint32(len(sp.payload))
			if rb.lastPayload == nil || len(rb.lastPayload) != len(sp.payload) {
				rb.lastPayload = make([]byte, len(sp.payload))
			}
			copy(rb.lastPayload, sp.payload)
		}
		rb.mu.Unlock()

		select {
		case rb.deliverCh <- deliverResult{header: sp.header, payload: sp.payload, n: len(sp.payload)}:
		case <-ctx.Done():
			return false
		}
	}
}

func (rb *RTPReorderBuffer) drainRemaining() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	for {
		sp, exists := rb.packets[rb.nextSeq]
		if !exists {
			return
		}
		delete(rb.packets, rb.nextSeq)
		rb.nextSeq++
		select {
		case rb.deliverCh <- deliverResult{header: sp.header, payload: sp.payload, n: len(sp.payload)}:
		default:
			return
		}
	}
}

func (rb *RTPReorderBuffer) sendDeliver(ctx context.Context, result deliverResult) bool {
	select {
	case rb.deliverCh <- result:
		return true
	case <-ctx.Done():
		return false
	}
}

func (rb *RTPReorderBuffer) ReadRTP(buf []byte, packet interface{}) (int, error) {
	rtpPacket, ok := packet.(*rtp.Packet)
	if !ok {
		return 0, fmt.Errorf("audio: reorder buffer: packet must be *rtp.Packet")
	}

	select {
	case result, ok := <-rb.deliverCh:
		if !ok {
			return 0, ErrBufferClosed
		}
		if result.err != nil {
			return 0, result.err
		}
		rtpPacket.Header = result.header
		n := copy(buf, result.payload)
		rtpPacket.Payload = buf[:n]
		return n, nil
	case <-rb.stopCh:
		return 0, ErrBufferClosed
	}
}

func (rb *RTPReorderBuffer) WriteRTP(packet interface{}) error {
	return rb.inner.WriteRTP(packet)
}

func (rb *RTPReorderBuffer) LocalAddr() net.Addr {
	return rb.inner.LocalAddr()
}

func (rb *RTPReorderBuffer) RemoteAddr() net.Addr {
	return rb.inner.RemoteAddr()
}

func (rb *RTPReorderBuffer) Close() error {
	rb.closeOnce.Do(func() {
		rb.logf("[reorder] call=%s Close() invoked — tearing down buffer + inner media session", rb.opts.CallID)
		rb.cancel()
		close(rb.stopCh)
		// Force the in-flight ReadRTP in readFromInner to return. Cancelling rb.ctx
		// cannot interrupt a blocking read already in progress, and inner.Close()
		// does not reliably wake a wedged/half-open RTP socket (mediaSessionAdapter.
		// Close just calls session.Close, which never sets a read deadline). Setting
		// a past read deadline via UnblockReaders makes ReadRTP return a timeout, so
		// readFromInner observes the cancelled ctx and exits — otherwise that reader
		// goroutine (and the diago RTP monitor behind it) leaks for the process life.
		if err := rb.inner.UnblockReaders(); err != nil {
			rb.logf("[reorder] call=%s Close() UnblockReaders failed (continuing teardown): %v", rb.opts.CallID, err)
		}
	})
	_ = rb.inner.Close()

	// Bounded wait for mainLoop to close doneCh. Close() MUST return even if the
	// inner socket is so wedged that neither UnblockReaders nor inner.Close() wakes
	// the reader: the call bridge releases the workspace call slot only after Close()
	// returns (`defer releaseCallSlot`), so an unbounded wait here leaks the slot
	// forever. With the Run() fix mainLoop exits on rb.cancel() (independent of the
	// reader), so a healthy drain completes in sub-milliseconds; the timeout is the
	// last-resort backstop that trades one abandoned reader goroutine for a
	// guaranteed slot release.
	//
	// Stats are logged here (not in the caller's dialog.OnClose) because Close is the
	// one teardown path guaranteed to run for every call, and doneCh is closed only
	// after mainLoop exits so they are final. logStatsOnce keeps it to a single line
	// even when Close is invoked from multiple paths (bridge + dialog.OnClose).
	select {
	case <-rb.doneCh:
	case <-time.After(closeDrainTimeout):
		rb.logf("[reorder] call=%s Close() drain timed out after %s; abandoning wedged RTP reader to avoid blocking call teardown (slot release)", rb.opts.CallID, closeDrainTimeout)
	}
	rb.logStatsOnce.Do(rb.logStats)
	return nil
}

// logStats emits a one-line per-call summary of inbound RTP health. loss% is the
// share of expected packets the buffer had to conceal: concealed / (received +
// concealed). Until this existed, Stats() was only read in tests, so production
// calls had no packet-loss visibility (the buffer otherwise only logs [reorder]
// lines on large/bursty gaps).
func (rb *RTPReorderBuffer) logStats() {
	if rb.opts.Logger == nil {
		return
	}
	st := rb.Stats()
	expected := st.Received + st.SilenceFilled
	lossPct := 0.0
	if expected > 0 {
		lossPct = 100 * float64(st.SilenceFilled) / float64(expected)
	}
	rb.opts.Logger("[reorder-stats] call=%s received=%d concealed=%d late_dropped=%d resyncs=%d loss=%.2f%%",
		rb.opts.CallID, st.Received, st.SilenceFilled, st.LateDroppedPackets, st.Resyncs, lossPct)
}

func (rb *RTPReorderBuffer) OnDTMF(handler voip.DTMFHandler) {
	rb.inner.OnDTMF(handler)
}

// NegotiatedCodec delegates to the wrapped session (the codec is decided once
// at the trunk layer; this wrapper only reorders packets).
func (rb *RTPReorderBuffer) NegotiatedCodec() voip.CodecInfo {
	return rb.inner.NegotiatedCodec()
}

func (rb *RTPReorderBuffer) UnblockReaders() error {
	return rb.inner.UnblockReaders()
}

func (rb *RTPReorderBuffer) Stats() ReorderBufferStats {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.stats
}

func seqDiff(curr, base uint16) int16 {
	return int16(curr - base)
}

var _ voip.MediaSession = (*RTPReorderBuffer)(nil)
