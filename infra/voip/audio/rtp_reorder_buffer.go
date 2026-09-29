package audio

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"

	"vozko/domain/voip"
)

const (
	rtpReadBufferSize  = 1500
	defaultQueueSize   = 50
	defaultReorderWait = 60 * time.Millisecond
)

type ReorderSource interface {
	voip.MediaSession
	voip.DTMFSource
	voip.CodecReporter
}

type RTPReorderBufferOptions struct {
	Depth     int
	MaxWait   time.Duration
	QueueSize int
}

type bufferedRead struct {
	packet *rtp.Packet
	err    error
}

type RTPReorderBuffer struct {
	source    ReorderSource
	sequencer *rtpSequencer

	arrivals  chan bufferedRead
	delivered chan bufferedRead
	interrupt chan struct{}
	closed    chan struct{}

	closeOnce sync.Once
	runOnce   sync.Once
	closeErr  error

	terminalMu  sync.Mutex
	terminalErr error

	lastPacketNanos atomic.Int64
	dropped         atomic.Uint64
}

func NewRTPReorderBuffer(source ReorderSource, opts RTPReorderBufferOptions) *RTPReorderBuffer {
	depth := max(opts.Depth, 1)
	maxWait := opts.MaxWait
	if maxWait <= 0 {
		maxWait = defaultReorderWait
	}
	queueSize := opts.QueueSize
	if queueSize <= 0 {
		queueSize = defaultQueueSize
	}
	return &RTPReorderBuffer{
		source:    source,
		sequencer: newRTPSequencer(depth, maxWait),
		arrivals:  make(chan bufferedRead, queueSize),
		delivered: make(chan bufferedRead, queueSize),
		interrupt: make(chan struct{}, 1),
		closed:    make(chan struct{}),
	}
}

func (b *RTPReorderBuffer) Run(ctx context.Context) {
	b.runOnce.Do(func() {
		go b.readSource()
		go b.sequence()
		go func() {
			select {
			case <-ctx.Done():
				_ = b.Close()
			case <-b.closed:
			}
		}()
	})
}

func (b *RTPReorderBuffer) readSource() {
	buf := make([]byte, rtpReadBufferSize)
	for {
		var packet rtp.Packet
		_, err := b.source.ReadRTP(buf, &packet)
		if err != nil {
			if isTransient(err) {
				continue
			}
			b.push(bufferedRead{err: err})
			return
		}
		b.lastPacketNanos.Store(time.Now().UnixNano())
		clone := &rtp.Packet{Header: packet.Header.Clone(), Payload: append([]byte(nil), packet.Payload...)}
		if !b.push(bufferedRead{packet: clone}) {
			return
		}
	}
}

func (b *RTPReorderBuffer) push(read bufferedRead) bool {
	select {
	case b.arrivals <- read:
		return true
	case <-b.closed:
		return false
	}
}

func (b *RTPReorderBuffer) sequence() {
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	for {
		select {
		case <-b.closed:
			return
		case read := <-b.arrivals:
			if read.err != nil {
				b.deliverAll(b.sequencer.Drain())
				b.finish(read.err)
				return
			}
			b.deliverAll(b.sequencer.Push(read.packet, time.Now()))
		case <-timer.C:
			b.deliverAll(b.sequencer.Expire(time.Now()))
		}
		if deadline, ok := b.sequencer.NextDeadline(); ok {
			timer.Reset(time.Until(deadline))
		}
	}
}

func (b *RTPReorderBuffer) deliverAll(packets []*rtp.Packet) {
	for _, packet := range packets {
		b.deliver(bufferedRead{packet: packet})
	}
}

func (b *RTPReorderBuffer) deliver(read bufferedRead) {
	for {
		select {
		case b.delivered <- read:
			return
		default:
		}
		select {
		case <-b.delivered:
			b.dropped.Add(1)
		default:
		}
	}
}

func (b *RTPReorderBuffer) finish(err error) {
	b.terminalMu.Lock()
	b.terminalErr = err
	b.terminalMu.Unlock()
	close(b.delivered)
}

func (b *RTPReorderBuffer) terminal() error {
	b.terminalMu.Lock()
	defer b.terminalMu.Unlock()
	if b.terminalErr != nil {
		return b.terminalErr
	}
	return net.ErrClosed
}

func (b *RTPReorderBuffer) ReadRTP(buf []byte, packet any) (int, error) {
	dst, ok := packet.(*rtp.Packet)
	if !ok {
		return 0, fmt.Errorf("reorder buffer: packet must be *rtp.Packet, got %T", packet)
	}
	select {
	case <-b.closed:
		return 0, net.ErrClosed
	case <-b.interrupt:
		return 0, os.ErrDeadlineExceeded
	case read, open := <-b.delivered:
		if !open {
			return 0, b.terminal()
		}
		n, err := read.packet.MarshalTo(buf)
		if err != nil {
			return 0, err
		}
		*dst = *read.packet
		return n, nil
	}
}

func (b *RTPReorderBuffer) WriteRTP(packet any) error {
	return b.source.WriteRTP(packet)
}

func (b *RTPReorderBuffer) LocalAddr() net.Addr {
	return b.source.LocalAddr()
}

func (b *RTPReorderBuffer) RemoteAddr() net.Addr {
	return b.source.RemoteAddr()
}

func (b *RTPReorderBuffer) OnDTMF(handler voip.DTMFHandler) {
	b.source.OnDTMF(handler)
}

func (b *RTPReorderBuffer) NegotiatedCodec() voip.CodecInfo {
	return b.source.NegotiatedCodec()
}

func (b *RTPReorderBuffer) UnblockReaders() error {
	select {
	case b.interrupt <- struct{}{}:
	default:
	}
	return nil
}

func (b *RTPReorderBuffer) LastPacketAt() time.Time {
	nanos := b.lastPacketNanos.Load()
	if nanos == 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos)
}

func (b *RTPReorderBuffer) Dropped() uint64 {
	return b.dropped.Load()
}

func (b *RTPReorderBuffer) Close() error {
	b.closeOnce.Do(func() {
		close(b.closed)
		b.closeErr = b.source.Close()
	})
	return b.closeErr
}

func isTransient(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
