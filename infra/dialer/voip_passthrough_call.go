package dialer

import (
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"
	"github.com/zaf/g711"

	conversation_domain "vozko/domain/conversation"
	"vozko/domain/voip"
	"vozko/infra/voip/audio"
)

const (
	passthroughSampleRate   = 8000
	passthroughFrameSamples = 160
	passthroughFrameBytes   = 160
)

type passthroughPacer struct {
	clockRate int
	started   bool
	nextSend  time.Time
	timer     *time.Timer
}

func newPassthroughPacer(clockRate int) *passthroughPacer {
	t := time.NewTimer(time.Hour)
	if !t.Stop() {
		<-t.C
	}
	return &passthroughPacer{clockRate: clockRate, timer: t}
}

func (p *passthroughPacer) Wait(ctx context.Context, samples int) error {
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
	wait := time.Until(p.nextSend)
	p.nextSend = p.nextSend.Add(duration)
	if wait <= 0 {
		if -wait > 200*time.Millisecond {
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
	p.timer.Reset(wait)
	select {
	case <-ctx.Done():
		p.timer.Stop()
		return ctx.Err()
	case <-p.timer.C:
		return nil
	}
}

// lead reports how far ahead of real time the next frame is scheduled, i.e. the
// buffered one-way latency the pacer is currently holding. <= 0 means on or
// behind schedule. The plain Wait() loop only resets when it falls BEHIND
// (wait <= 0); nothing bounds how far AHEAD nextSend can drift, so when the
// browser's audio clock outpaces our wall clock even slightly, this lead, and
// the audible delay, grows for the whole call. SendAudio reads this to decimate.
func (p *passthroughPacer) lead() time.Duration {
	if p == nil || !p.started {
		return 0
	}
	return time.Until(p.nextSend)
}

// dropFrame pulls the send schedule back by one frame's worth of time, used when
// SendAudio decimates (drops) a frame to catch up; it keeps the pacer's notion of
// "now" aligned with the audio actually transmitted.
func (p *passthroughPacer) dropFrame(samples int) {
	if p == nil || !p.started || p.clockRate <= 0 || samples <= 0 {
		return
	}
	d := time.Duration(samples) * time.Second / time.Duration(p.clockRate)
	p.nextSend = p.nextSend.Add(-d)
}

// passthroughMaxUplinkLead bounds the buffered browser→SIP latency. Once the
// pacer's scheduling lead exceeds this, SendAudio drops frames to catch up so the
// one-way delay stays bounded instead of growing all call. ~120 ms is well under
// the "laggy" threshold for a live call and far above normal jitter, so
// decimation never triggers under healthy clock alignment.
const passthroughMaxUplinkLead = 120 * time.Millisecond

// passthroughUplinkStatsEvery is how many processed uplink frames (~20 ms each)
// between [VoipPassthrough] uplink stat lines (~5 s).
const passthroughUplinkStatsEvery = 250

type VoipPassthroughCall struct {
	id    string
	phone string
	media voip.MediaSession
	log   *log.Logger

	hangupTrunk func() error

	events  chan conversation_domain.CallEvent
	audioIn chan []byte
	done    chan struct{}

	ctx    context.Context
	cancel context.CancelFunc

	writeMu        sync.Mutex
	seqNum         uint16
	timestamp      uint32
	ssrc           uint32
	writePacer     *passthroughPacer
	pendingEncoded []byte
	codec          audio.Codec // negotiated G.711 codec, resolved lazily from media

	// uplink instrumentation (all guarded by writeMu)
	sentFrames     uint64
	droppedFrames  uint64
	framesSinceLog int

	hangupOnce sync.Once

	// surrender hands the raw media to an external RTP bridge (a blind transfer of
	// an AI-surrendered call to a branch). Once surrendered this call stops its own
	// read/write and parks until Hangup; the bridge is the sole reader/writer.
	surrendered     atomic.Bool
	readStopped     chan struct{}
	readStoppedOnce sync.Once
}

func (c *VoipPassthroughCall) markReadStopped() {
	c.readStoppedOnce.Do(func() { close(c.readStopped) })
}

func NewVoipPassthroughCall(id, phone string, media voip.MediaSession, hangupTrunk func() error, logger *log.Logger) *VoipPassthroughCall {
	if logger == nil {
		logger = log.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &VoipPassthroughCall{
		id:          id,
		phone:       phone,
		media:       media,
		hangupTrunk: hangupTrunk,
		log:         logger,
		events:      make(chan conversation_domain.CallEvent, 8),
		audioIn:     make(chan []byte, 200),
		done:        make(chan struct{}),
		readStopped: make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
		ssrc:        uint32(time.Now().UnixNano() & 0xFFFFFFFF),
	}
}

func (c *VoipPassthroughCall) Start() {
	c.log.Printf("[VoipPassthrough] %s Start(), human leg attached to the surrendered SIP media", c.id)
	select {
	case c.events <- conversation_domain.CallEvent{Type: conversation_domain.CallEventAnswered}:
	default:
	}
	go c.readLoop()
}

func (c *VoipPassthroughCall) ID() string                                   { return c.id }
func (c *VoipPassthroughCall) AudioStream() <-chan []byte                   { return c.audioIn }
func (c *VoipPassthroughCall) Events() <-chan conversation_domain.CallEvent { return c.events }
func (c *VoipPassthroughCall) Done() <-chan struct{}                        { return c.done }

func (c *VoipPassthroughCall) readLoop() {
	c.log.Printf("[VoipPassthrough] %s readLoop started, reading surrendered media for the human leg", c.id)
	firstPacketLogged := false
	defer func() {
		c.log.Printf("[VoipPassthrough] %s readLoop exited (ctxErr=%v), human leg media stopped", c.id, c.ctx.Err())
		close(c.done)

		time.Sleep(50 * time.Millisecond)
		close(c.audioIn)
		close(c.events)
	}()

	buf := make([]byte, 1500)
	var dlReceived, dlDropped, dlSinceLog uint64
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}
		// Stop deterministically on surrender before the next read (see the trunk
		// readSIPAudio note): a wrapping reorder buffer can absorb the UnblockReaders
		// timeout, so checking the flag at the loop top hands the session to the
		// external bridge within one packet instead of waiting out the timeout.
		if c.surrendered.Load() {
			c.markReadStopped()
			<-c.ctx.Done()
			return
		}
		packet := &rtp.Packet{}
		if _, err := c.media.ReadRTP(buf, packet); err != nil {
			// Surrender wakes this read on purpose (UnblockReaders) so an external RTP
			// bridge can take over; park until Hangup so done/audioIn stay open and the
			// caller keeps talking to the phone through the bridge.
			if c.surrendered.Load() {
				c.markReadStopped()
				<-c.ctx.Done()
				return
			}
			if c.ctx.Err() == nil {
				c.log.Printf("[VoipPassthrough] %s ReadRTP error: %v", c.id, err)
			}
			return
		}
		if !firstPacketLogged {
			firstPacketLogged = true
			c.log.Printf("[VoipPassthrough] %s first inbound RTP on human leg (pt=%d), caller↔human media is flowing", c.id, packet.PayloadType)
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
		dlReceived++
		select {
		case c.audioIn <- pcm16:
		default:
			// Browser→WS downlink can't keep up: drop the newest caller frame rather
			// than block the RTP read. A climbing dlDropped means the agent's browser
			// isn't draining caller audio fast enough (downlink congestion).
			dlDropped++
		}
		// Periodic caller→human (downlink) heartbeat, mirroring the uplink stats.
		dlSinceLog++
		if dlSinceLog >= passthroughUplinkStatsEvery {
			dlSinceLog = 0
			c.log.Printf("[VoipPassthrough] %s downlink stats: caller_rtp_received=%d dropped_to_browser=%d",
				c.id, dlReceived, dlDropped)
		}
	}
}

func (c *VoipPassthroughCall) SendAudio(pcm16 []byte) error {
	if len(pcm16) == 0 {
		return nil
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	// The codec is decided by the SIP trunk and carried on the media session;
	// resolve it once so the dialer encodes with whatever the carrier negotiated
	// (no per-channel divergence). See docs/SIP_AUDIO_PIPELINE.md §10 (P1).
	if c.codec == nil {
		name := ""
		if c.media != nil {
			name = c.media.NegotiatedCodec().Name
		}
		c.codec = audio.G711CodecForName(name)
	}

	encoded := c.codec.Encode(pcm16)
	if len(c.pendingEncoded) > 0 {
		combined := make([]byte, 0, len(c.pendingEncoded)+len(encoded))
		combined = append(combined, c.pendingEncoded...)
		combined = append(combined, encoded...)
		encoded = combined
		c.pendingEncoded = c.pendingEncoded[:0]
	}
	if c.writePacer == nil {
		c.writePacer = newPassthroughPacer(passthroughSampleRate)
	}

	offset := 0
	for offset+passthroughFrameBytes <= len(encoded) {
		// Bound one-way latency: if the pacer has drifted too far ahead of real
		// time (the browser's audio clock outpacing our send clock, the cause of
		// the ever-growing transfer-call delay), decimate. Drop the oldest queued
		// frame so the backlog actually shrinks. We do NOT advance the RTP
		// timestamp/seq here: the dropped audio is discarded, and transmitted
		// frames stay contiguous so the receiver plays them back-to-back (the
		// stream is 20 ms shorter, i.e. caught up) rather than inserting a gap.
		if c.writePacer.lead() > passthroughMaxUplinkLead {
			offset += passthroughFrameBytes
			c.writePacer.dropFrame(passthroughFrameSamples)
			c.droppedFrames++
			c.maybeLogUplinkStatsLocked()
			continue
		}

		if err := c.writePacer.Wait(c.ctx, passthroughFrameSamples); err != nil {
			c.pendingEncoded = append(c.pendingEncoded[:0], encoded[offset:]...)
			return err
		}
		frame := encoded[offset : offset+passthroughFrameBytes]
		pkt := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    c.codec.PayloadType(),
				SequenceNumber: c.seqNum,
				Timestamp:      c.timestamp,
				SSRC:           c.ssrc,
			},
			Payload: frame,
		}
		if err := c.media.WriteRTP(pkt); err != nil {
			c.pendingEncoded = append(c.pendingEncoded[:0], encoded[offset:]...)
			return err
		}
		c.seqNum++
		c.timestamp += passthroughFrameSamples
		c.sentFrames++
		c.maybeLogUplinkStatsLocked()
		offset += passthroughFrameBytes
	}
	if offset < len(encoded) {

		tail := encoded[offset:]
		// Cap carried-over audio tightly: a large pending buffer (the old 100-frame
		// / 2 s cap) re-injects seconds of latency on the next call. The normal
		// remainder is a sub-frame, so 8 frames (160 ms) is ample headroom while
		// keeping delay bounded if a mid-stream Wait/WriteRTP error saved a backlog.
		const maxPending = passthroughFrameBytes * 8
		if len(tail) > maxPending {
			tail = tail[len(tail)-maxPending:]
		}
		c.pendingEncoded = append(c.pendingEncoded[:0], tail...)
	}
	return nil
}

// maybeLogUplinkStatsLocked emits a periodic browser→SIP health line (sent /
// decimated frame counts, current pacer lead = buffered latency, pending bytes).
// Caller must hold writeMu. A climbing pacer_lead with no decimation means a slow
// send clock; a climbing decimated count means the browser clock is outpacing us.
func (c *VoipPassthroughCall) maybeLogUplinkStatsLocked() {
	c.framesSinceLog++
	if c.framesSinceLog < passthroughUplinkStatsEvery {
		return
	}
	c.framesSinceLog = 0
	var lead time.Duration
	if c.writePacer != nil {
		lead = c.writePacer.lead()
	}
	c.log.Printf("[VoipPassthrough] %s uplink stats: sent=%d decimated=%d pacer_lead=%dms pending=%dB",
		c.id, c.sentFrames, c.droppedFrames, lead.Milliseconds(), len(c.pendingEncoded))
}

// SurrenderMedia stops this call's own read/write loops and hands the raw media
// session to an external RTP bridge, keeping the leg alive until Hangup. It mirrors
// sipTrunkCRMCall.SurrenderMedia and is idempotent, satisfying the surrender contract
// that dialer_domain.CallLeg.SurrenderMedia expects.
func (c *VoipPassthroughCall) SurrenderMedia() (voip.MediaSession, error) {
	if !c.surrendered.CompareAndSwap(false, true) {
		return nil, errors.New("passthrough call: media already surrendered")
	}
	if c.media == nil {
		return nil, errors.New("passthrough call: no media session to surrender")
	}
	if err := c.media.UnblockReaders(); err != nil {
		c.log.Printf("[VoipPassthrough] %s surrender: UnblockReaders failed (proceeding): %v", c.id, err)
	}
	select {
	case <-c.readStopped:
	case <-time.After(2 * time.Second):
		c.log.Printf("[VoipPassthrough] %s surrender: timed out waiting for the read loop to stop", c.id)
	}
	return c.media, nil
}

func (c *VoipPassthroughCall) Hangup() error {
	var err error
	c.hangupOnce.Do(func() {
		c.log.Printf("[VoipPassthrough] %s Hangup() invoked, cancelling read, closing media, sending SIP BYE", c.id)
		c.cancel()
		if c.media != nil {
			if cerr := c.media.Close(); cerr != nil {
				err = cerr
			}
		}
		if c.hangupTrunk != nil {
			if berr := c.hangupTrunk(); berr != nil {
				c.log.Printf("[VoipPassthroughCall] SIP BYE failed for call %s: %v", c.id, berr)
				if err == nil {
					err = berr
				}
			}
		}
	})
	return err
}
