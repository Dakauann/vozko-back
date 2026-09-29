package audio

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtp"

	"vozko/domain/voip"
)

type fakeSource struct {
	packets chan *rtp.Packet
	errs    chan error
	written []*rtp.Packet
	mu      sync.Mutex
	closed  chan struct{}
	once    sync.Once
	dtmf    voip.DTMFHandler
	codec   voip.CodecInfo
}

func newFakeSource() *fakeSource {
	return &fakeSource{packets: make(chan *rtp.Packet, 64), errs: make(chan error, 4), closed: make(chan struct{})}
}

func (f *fakeSource) ReadRTP(buf []byte, packet any) (int, error) {
	dst := packet.(*rtp.Packet)
	select {
	case p := <-f.packets:
		n, err := p.MarshalTo(buf)
		if err != nil {
			return 0, err
		}
		if err := dst.Unmarshal(buf[:n]); err != nil {
			return 0, err
		}
		return n, nil
	case err := <-f.errs:
		return 0, err
	case <-f.closed:
		return 0, net.ErrClosed
	}
}

func (f *fakeSource) WriteRTP(packet any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.written = append(f.written, packet.(*rtp.Packet))
	return nil
}

func (f *fakeSource) LocalAddr() net.Addr  { return &net.UDPAddr{} }
func (f *fakeSource) RemoteAddr() net.Addr { return &net.UDPAddr{} }
func (f *fakeSource) Close() error {
	f.once.Do(func() { close(f.closed) })
	return nil
}
func (f *fakeSource) OnDTMF(handler voip.DTMFHandler) { f.dtmf = handler }
func (f *fakeSource) NegotiatedCodec() voip.CodecInfo { return f.codec }

func voicePacket(seq uint16) *rtp.Packet {
	return &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: seq, PayloadType: 0}, Payload: []byte{byte(seq), 1, 2, 3}}
}

func startBuffer(t *testing.T, source *fakeSource, opts RTPReorderBufferOptions) *RTPReorderBuffer {
	t.Helper()
	buffer := NewRTPReorderBuffer(source, opts)
	buffer.Run(context.Background())
	t.Cleanup(func() { _ = buffer.Close() })
	return buffer
}

func readSeq(t *testing.T, buffer *RTPReorderBuffer) uint16 {
	t.Helper()
	type result struct {
		p   rtp.Packet
		err error
	}
	done := make(chan result, 1)
	go func() {
		var p rtp.Packet
		_, err := buffer.ReadRTP(make([]byte, 1500), &p)
		done <- result{p, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("ReadRTP() error = %v", r.err)
		}
		return r.p.SequenceNumber
	case <-time.After(2 * time.Second):
		t.Fatal("ReadRTP() did not return")
	}
	return 0
}

func TestReorderBufferDeliversPacketsInSequenceOrder(t *testing.T) {
	source := newFakeSource()
	buffer := startBuffer(t, source, RTPReorderBufferOptions{Depth: 3, MaxWait: 40 * time.Millisecond})
	for _, seq := range []uint16{1, 3, 2, 4, 5} {
		source.packets <- voicePacket(seq)
	}
	for want := uint16(1); want <= 5; want++ {
		if got := readSeq(t, buffer); got != want {
			t.Fatalf("read seq %d, want %d", got, want)
		}
	}
}

func TestReorderBufferReleasesAHeldPacketAfterMaxWait(t *testing.T) {
	source := newFakeSource()
	buffer := startBuffer(t, source, RTPReorderBufferOptions{Depth: 3, MaxWait: 20 * time.Millisecond})
	source.packets <- voicePacket(7)
	if got := readSeq(t, buffer); got != 7 {
		t.Fatalf("read seq %d, want 7", got)
	}
}

func TestReorderBufferReturnsTheCopiedPacketBytesInTheCallerBuffer(t *testing.T) {
	source := newFakeSource()
	buffer := startBuffer(t, source, RTPReorderBufferOptions{Depth: 1, MaxWait: 10 * time.Millisecond})
	source.packets <- voicePacket(9)
	buf := make([]byte, 1500)
	var p rtp.Packet
	n, err := buffer.ReadRTP(buf, &p)
	if err != nil {
		t.Fatalf("ReadRTP() error = %v", err)
	}
	var parsed rtp.Packet
	if err := parsed.Unmarshal(buf[:n]); err != nil || parsed.SequenceNumber != 9 || len(parsed.Payload) != 4 {
		t.Fatalf("buffer bytes parsed to seq %d payload %v err %v", parsed.SequenceNumber, parsed.Payload, err)
	}
}

func TestReorderBufferDropsTheOldestPacketsWhenTheConsumerFallsBehind(t *testing.T) {
	source := newFakeSource()
	buffer := startBuffer(t, source, RTPReorderBufferOptions{Depth: 1, MaxWait: 5 * time.Millisecond, QueueSize: 2})
	for seq := uint16(1); seq <= 6; seq++ {
		source.packets <- voicePacket(seq)
	}
	deadline := time.Now().Add(2 * time.Second)
	for buffer.Dropped() < 4 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := buffer.Dropped(); got != 4 {
		t.Fatalf("Dropped() = %d, want 4", got)
	}
	if got := readSeq(t, buffer); got != 5 {
		t.Fatalf("first read seq %d, want the freshest queued packet 5", got)
	}
}

func TestReorderBufferReportsTheLastPacketTime(t *testing.T) {
	source := newFakeSource()
	buffer := startBuffer(t, source, RTPReorderBufferOptions{Depth: 1, MaxWait: 5 * time.Millisecond})
	if !buffer.LastPacketAt().IsZero() {
		t.Fatal("LastPacketAt() is set before any packet arrived")
	}
	before := time.Now()
	source.packets <- voicePacket(1)
	readSeq(t, buffer)
	if buffer.LastPacketAt().Before(before) {
		t.Fatalf("LastPacketAt() = %v, want at or after %v", buffer.LastPacketAt(), before)
	}
}

func TestReorderBufferUnblockReadersWakesAWaitingReaderAndKeepsRunning(t *testing.T) {
	source := newFakeSource()
	buffer := startBuffer(t, source, RTPReorderBufferOptions{Depth: 1, MaxWait: 5 * time.Millisecond})
	errCh := make(chan error, 1)
	go func() {
		var p rtp.Packet
		_, err := buffer.ReadRTP(make([]byte, 1500), &p)
		errCh <- err
	}()
	time.Sleep(20 * time.Millisecond)
	if err := buffer.UnblockReaders(); err != nil {
		t.Fatalf("UnblockReaders() error = %v", err)
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("woken reader error = %v, want os.ErrDeadlineExceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("UnblockReaders() did not wake the reader")
	}
	source.packets <- voicePacket(3)
	if got := readSeq(t, buffer); got != 3 {
		t.Fatalf("read seq %d after unblock, want 3", got)
	}
}

func TestReorderBufferSurfacesTheTerminalSourceErrorAfterDraining(t *testing.T) {
	source := newFakeSource()
	buffer := startBuffer(t, source, RTPReorderBufferOptions{Depth: 3, MaxWait: time.Second})
	source.packets <- voicePacket(1)
	time.Sleep(20 * time.Millisecond)
	source.errs <- io.ErrUnexpectedEOF
	if got := readSeq(t, buffer); got != 1 {
		t.Fatalf("read seq %d, want the drained packet 1", got)
	}
	var p rtp.Packet
	if _, err := buffer.ReadRTP(make([]byte, 1500), &p); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("ReadRTP() after source failure = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestReorderBufferCloseUnblocksReadersAndClosesTheSource(t *testing.T) {
	source := newFakeSource()
	buffer := NewRTPReorderBuffer(source, RTPReorderBufferOptions{Depth: 3, MaxWait: time.Second})
	buffer.Run(context.Background())
	errCh := make(chan error, 1)
	go func() {
		var p rtp.Packet
		_, err := buffer.ReadRTP(make([]byte, 1500), &p)
		errCh <- err
	}()
	time.Sleep(10 * time.Millisecond)
	if err := buffer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := buffer.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("reader error after Close = %v, want net.ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close() did not unblock the reader")
	}
	select {
	case <-source.closed:
	default:
		t.Fatal("Close() did not close the source")
	}
}

func TestReorderBufferStopsWhenTheRunContextEnds(t *testing.T) {
	source := newFakeSource()
	buffer := NewRTPReorderBuffer(source, RTPReorderBufferOptions{Depth: 3, MaxWait: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	buffer.Run(ctx)
	cancel()
	select {
	case <-source.closed:
	case <-time.After(time.Second):
		t.Fatal("ending the run context did not close the source")
	}
}

func TestReorderBufferForwardsWritesAndCapabilities(t *testing.T) {
	source := newFakeSource()
	source.codec = voip.CodecInfo{Name: "PCMU"}
	buffer := startBuffer(t, source, RTPReorderBufferOptions{Depth: 3, MaxWait: time.Second})
	if err := buffer.WriteRTP(voicePacket(1)); err != nil {
		t.Fatalf("WriteRTP() error = %v", err)
	}
	if len(source.written) != 1 {
		t.Fatalf("source received %d writes, want 1", len(source.written))
	}
	if got := buffer.NegotiatedCodec().Name; got != "PCMU" {
		t.Fatalf("NegotiatedCodec() = %q, want PCMU", got)
	}
	fired := false
	buffer.OnDTMF(func(rune) { fired = true })
	source.dtmf('5')
	if !fired {
		t.Fatal("OnDTMF() handler was not forwarded to the source")
	}
}
