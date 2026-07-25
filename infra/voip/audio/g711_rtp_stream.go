package audio

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"
)

const (
	DefaultSampleRate  = 8000
	DefaultFrameDur    = 20 * time.Millisecond
	DefaultFrameBytes  = 160
	DefaultPayloadType = uint8(0)

	DefaultBufferCap = 16000

	MaxBehindBeforeReset = 10 * DefaultFrameDur

	MulawSilence = byte(0xFF)
)

var ErrStreamClosed = errors.New("audio: mulaw rtp stream closed")

type RTPMediaWriter interface {
	WriteRTP(packet interface{}) error
}

type Metrics struct {
	AudioFrames     uint64
	PartialFrames   uint64
	SilenceFrames   uint64
	BytesEnqueued   uint64
	BytesEmitted    uint64
	Overflows       uint64
	WriteRTPErrors  uint64
	LastWriteRTPErr error
}

type Options struct {
	PayloadType uint8

	// Codec selects the G.711 companding law for outbound encoding and
	// silence/underrun fill. When nil the stream defaults to µ-law (PCMU),
	// preserving historical behaviour. When set, it overrides PayloadType with
	// the codec's own payload type so the wire label always matches the bytes.
	Codec Codec

	FrameDur time.Duration

	SampleRate int

	BufferCap int

	SSRC       uint32
	InitialSeq uint16
	InitialTS  uint32

	// BackgroundBed, when non-empty, is a pre-attenuated mono PCM16 loop mixed
	// softly under every outbound frame for the whole stream lifetime. The slice
	// is shared read-only across streams; each stream keeps its own loop cursor.
	BackgroundBed []int16

	Logger func(format string, args ...interface{})
}

type G711RTPStream struct {
	media       RTPMediaWriter
	codec       Codec
	silenceByte byte
	payloadType uint8
	sampleRate  int
	frameDur    time.Duration
	frameBytes  int
	ssrc        uint32
	bg          *backgroundBed
	logger      func(string, ...interface{})

	mu      sync.Mutex
	notFull *sync.Cond
	drained *sync.Cond
	buf     []byte
	bufCap  int
	closed  bool
	pkt     rtp.Packet
	seq     uint16
	ts      uint32

	metrics struct {
		audioFrames    atomic.Uint64
		partialFrames  atomic.Uint64
		silenceFrames  atomic.Uint64
		bytesEnqueued  atomic.Uint64
		bytesEmitted   atomic.Uint64
		overflows      atomic.Uint64
		writeRTPErrors atomic.Uint64
	}
	lastWriteErr atomic.Value

	runOnce   sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
	doneCh    chan struct{}
	runCancel context.CancelFunc
}

func New(media RTPMediaWriter, opts Options) *G711RTPStream {
	if opts.SampleRate <= 0 {
		opts.SampleRate = DefaultSampleRate
	}
	if opts.FrameDur <= 0 {
		opts.FrameDur = DefaultFrameDur
	}
	if opts.BufferCap <= 0 {
		opts.BufferCap = DefaultBufferCap
	}

	frameBytes := int(int64(opts.FrameDur) * int64(opts.SampleRate) / int64(time.Second))
	if frameBytes <= 0 {
		frameBytes = DefaultFrameBytes
	}

	codec := opts.Codec
	payloadType := opts.PayloadType
	if codec == nil {
		codec = CodecMulaw
	} else {
		// Keep the RTP payload-type label consistent with the encoded bytes.
		payloadType = codec.PayloadType()
	}

	s := &G711RTPStream{
		media:       media,
		codec:       codec,
		silenceByte: codec.SilenceByte(),
		payloadType: payloadType,
		sampleRate:  opts.SampleRate,
		frameDur:    opts.FrameDur,
		frameBytes:  frameBytes,
		ssrc:        opts.SSRC,
		bg:          newBackgroundBed(opts.BackgroundBed),
		bufCap:      opts.BufferCap,
		seq:         opts.InitialSeq,
		ts:          opts.InitialTS,
		logger:      opts.Logger,
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
	}
	s.notFull = sync.NewCond(&s.mu)
	s.drained = sync.NewCond(&s.mu)
	return s
}

func (s *G711RTPStream) FrameBytes() int { return s.frameBytes }

func (s *G711RTPStream) FrameDur() time.Duration { return s.frameDur }

func (s *G711RTPStream) BufferedBytes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.buf)
}

func (s *G711RTPStream) DrainBuffer() {
	s.mu.Lock()
	if len(s.buf) > 0 {
		s.buf = s.buf[:0]
	}
	s.notFull.Broadcast()
	s.drained.Broadcast()
	s.mu.Unlock()
}

func (s *G711RTPStream) WritePCM16(ctx context.Context, pcm16 []byte) error {
	if len(pcm16) == 0 {
		return nil
	}
	return s.WriteEncoded(ctx, s.codec.Encode(pcm16))
}

func (s *G711RTPStream) WriteEncoded(ctx context.Context, encoded []byte) error {
	if len(encoded) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	cancelHelper := func() func() {
		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				s.mu.Lock()
				s.notFull.Broadcast()
				s.mu.Unlock()
			case <-done:
			}
		}()
		return func() { close(done) }
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for len(encoded) > 0 {
		if s.closed {
			return ErrStreamClosed
		}
		space := s.bufCap - len(s.buf)
		if space <= 0 {
			s.metrics.overflows.Add(1)
			cancel := cancelHelper()
			s.notFull.Wait()
			cancel()
			if err := ctx.Err(); err != nil {
				return err
			}
			continue
		}
		n := len(encoded)
		if n > space {
			n = space
		}
		s.buf = append(s.buf, encoded[:n]...)
		s.metrics.bytesEnqueued.Add(uint64(n))
		encoded = encoded[n:]
	}
	return nil
}

func (s *G711RTPStream) Step() error {
	s.mu.Lock()

	payload := make([]byte, s.frameBytes)
	avail := len(s.buf)
	switch {
	case avail >= s.frameBytes:
		copy(payload, s.buf[:s.frameBytes])

		s.buf = s.buf[s.frameBytes:]
		s.metrics.audioFrames.Add(1)
		s.metrics.bytesEmitted.Add(uint64(s.frameBytes))
	case avail > 0:

		copy(payload, s.buf[:avail])
		for i := avail; i < s.frameBytes; i++ {
			payload[i] = s.silenceByte
		}
		s.buf = s.buf[:0]
		s.metrics.partialFrames.Add(1)
		s.metrics.bytesEmitted.Add(uint64(avail))
	default:
		for i := range payload {
			payload[i] = s.silenceByte
		}
		s.metrics.silenceFrames.Add(1)
	}

	// Fold the looped ambience under the frame (speech, partial or silence)
	// before it goes on the wire, so the bed is continuous for the whole call.
	s.bg.mixFrame(payload, s.codec)

	s.pkt.Header = rtp.Header{
		Version:        2,
		PayloadType:    s.payloadType,
		SequenceNumber: s.seq,
		Timestamp:      s.ts,
		SSRC:           s.ssrc,
	}
	s.pkt.Payload = payload
	s.seq++
	s.ts += uint32(s.frameBytes)

	s.notFull.Broadcast()
	if len(s.buf) == 0 {
		s.drained.Broadcast()
	}

	media := s.media
	s.mu.Unlock()

	if media == nil {
		return nil
	}
	if err := media.WriteRTP(&s.pkt); err != nil {
		s.metrics.writeRTPErrors.Add(1)
		s.lastWriteErr.Store(err)
		return err
	}
	return nil
}

func (s *G711RTPStream) Drain(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	cancelHelper := func() func() {
		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				s.mu.Lock()
				s.drained.Broadcast()
				s.mu.Unlock()
			case <-done:
			}
		}()
		return func() { close(done) }
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.buf) > 0 {
		if s.closed {
			return ErrStreamClosed
		}
		cancel := cancelHelper()
		s.drained.Wait()
		cancel()
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (s *G711RTPStream) Run(ctx context.Context) {
	s.runOnce.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		runCtx, cancel := context.WithCancel(ctx)
		s.runCancel = cancel
		go s.emitLoop(runCtx)
	})
}

func (s *G711RTPStream) emitLoop(ctx context.Context) {
	defer close(s.doneCh)

	timer := time.NewTimer(s.frameDur)
	defer timer.Stop()

	next := time.Now().Add(s.frameDur)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-timer.C:
		}

		_ = s.Step()

		next = next.Add(s.frameDur)
		delay := time.Until(next)
		if delay < 0 {
			if -delay > MaxBehindBeforeReset {
				next = time.Now().Add(s.frameDur)
				delay = s.frameDur
				if s.logger != nil {
					s.logger("[audio] emit loop resynced after long stall")
				}
			} else {
				delay = 0
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(delay)
	}
}

func (s *G711RTPStream) Stop() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.notFull.Broadcast()
		s.drained.Broadcast()
		s.mu.Unlock()
		close(s.stopCh)
		if s.runCancel != nil {
			s.runCancel()
		}
	})
}

func (s *G711RTPStream) Wait() {
	select {
	case <-s.doneCh:
	default:
		if s.runCancel == nil {
			return
		}
		<-s.doneCh
	}
}

func (s *G711RTPStream) Metrics() Metrics {
	m := Metrics{
		AudioFrames:    s.metrics.audioFrames.Load(),
		PartialFrames:  s.metrics.partialFrames.Load(),
		SilenceFrames:  s.metrics.silenceFrames.Load(),
		BytesEnqueued:  s.metrics.bytesEnqueued.Load(),
		BytesEmitted:   s.metrics.bytesEmitted.Load(),
		Overflows:      s.metrics.overflows.Load(),
		WriteRTPErrors: s.metrics.writeRTPErrors.Load(),
	}
	if v := s.lastWriteErr.Load(); v != nil {
		if err, ok := v.(error); ok {
			m.LastWriteRTPErr = err
		}
	}
	return m
}
