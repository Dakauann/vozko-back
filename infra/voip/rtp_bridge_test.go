package voipinfra

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtp"

	"vozko/domain/voip"
)

// fakeMediaSession is an in-memory voip.MediaSession: packets pushed to inbound are
// returned by ReadRTP; WriteRTP records to written. UnblockReaders makes a blocked
// ReadRTP return an error, exactly like the real diago session teardown.
type fakeMediaSession struct {
	inbound  chan *rtp.Packet
	written  chan *rtp.Packet
	unblock  chan struct{}
	closeOne sync.Once
}

func newFakeMediaSession() *fakeMediaSession {
	return &fakeMediaSession{
		inbound: make(chan *rtp.Packet, 16),
		written: make(chan *rtp.Packet, 16),
		unblock: make(chan struct{}),
	}
}

func (f *fakeMediaSession) ReadRTP(buf []byte, packet interface{}) (int, error) {
	p, ok := packet.(*rtp.Packet)
	if !ok {
		return 0, errors.New("packet must be *rtp.Packet")
	}
	select {
	case in := <-f.inbound:
		*p = *in
		return len(p.Payload), nil
	case <-f.unblock:
		return 0, errors.New("readers unblocked")
	}
}

func (f *fakeMediaSession) WriteRTP(packet interface{}) error {
	p, ok := packet.(*rtp.Packet)
	if !ok {
		return errors.New("packet must be *rtp.Packet")
	}
	cp := *p
	select {
	case f.written <- &cp:
	default:
	}
	return nil
}

func (f *fakeMediaSession) LocalAddr() net.Addr             { return nil }
func (f *fakeMediaSession) RemoteAddr() net.Addr            { return nil }
func (f *fakeMediaSession) Close() error                    { return nil }
func (f *fakeMediaSession) OnDTMF(handler voip.DTMFHandler) {}
func (f *fakeMediaSession) UnblockReaders() error {
	f.closeOne.Do(func() { close(f.unblock) })
	return nil
}
func (f *fakeMediaSession) NegotiatedCodec() voip.CodecInfo { return voip.CodecInfo{} }

func pcmaPacket(seq uint16, payload []byte) *rtp.Packet {
	return &rtp.Packet{
		Header:  rtp.Header{PayloadType: 8, SequenceNumber: seq},
		Payload: payload,
	}
}

func TestRTPBridge_RelaysBothDirections(t *testing.T) {
	a := newFakeMediaSession()
	b := newFakeMediaSession()
	br := NewRTPBridge(a, b, nil)
	br.Start(context.Background())
	defer br.Stop()

	// A -> B
	a.inbound <- pcmaPacket(1, []byte{0x01, 0x02, 0x03})
	select {
	case got := <-b.written:
		if got.SequenceNumber != 1 || len(got.Payload) != 3 {
			t.Fatalf("A->B relayed wrong packet: seq=%d len=%d", got.SequenceNumber, len(got.Payload))
		}
	case <-time.After(time.Second):
		t.Fatal("A->B packet was not relayed to B")
	}

	// B -> A
	b.inbound <- pcmaPacket(2, []byte{0x0a, 0x0b})
	select {
	case got := <-a.written:
		if got.SequenceNumber != 2 || len(got.Payload) != 2 {
			t.Fatalf("B->A relayed wrong packet: seq=%d len=%d", got.SequenceNumber, len(got.Payload))
		}
	case <-time.After(time.Second):
		t.Fatal("B->A packet was not relayed to A")
	}
}

func TestRTPBridge_DropsEmptyPayload(t *testing.T) {
	a := newFakeMediaSession()
	b := newFakeMediaSession()
	br := NewRTPBridge(a, b, nil)
	br.Start(context.Background())
	defer br.Stop()

	a.inbound <- pcmaPacket(1, nil)                 // empty: must NOT be relayed
	a.inbound <- pcmaPacket(2, []byte{0x01})        // real: must be relayed

	select {
	case got := <-b.written:
		if got.SequenceNumber != 2 {
			t.Fatalf("expected the non-empty packet (seq 2) to be relayed first, got seq %d", got.SequenceNumber)
		}
	case <-time.After(time.Second):
		t.Fatal("non-empty packet was not relayed")
	}
}

func TestRTPBridge_ReframesOversizedPayload(t *testing.T) {
	a := newFakeMediaSession()
	b := newFakeMediaSession()
	br := NewRTPBridge(a, b, nil)
	br.Start(context.Background())
	defer br.Stop()

	// The agent simulator emits a single ~2048-byte PCMU frame (its channel media has
	// no MTU). It must be split into standard 160-byte frames the phone accepts — not
	// dropped, and not fatal to the call (the old behaviour: a "short buffer" that
	// killed the whole bridge).
	big := make([]byte, 2048)
	for i := range big {
		big[i] = byte(i)
	}
	a.inbound <- &rtp.Packet{
		Header:  rtp.Header{PayloadType: 0, SequenceNumber: 7000, Timestamp: 1000},
		Payload: big,
	}

	var frames []*rtp.Packet
	total := 0
	deadline := time.After(2 * time.Second)
	for total < len(big) {
		select {
		case f := <-b.written:
			if len(f.Payload) > g711FrameBytes {
				t.Fatalf("reframed frame too large: %d bytes (want <= %d)", len(f.Payload), g711FrameBytes)
			}
			frames = append(frames, f)
			total += len(f.Payload)
		case <-deadline:
			t.Fatalf("only relayed %d/%d bytes across %d frames", total, len(big), len(frames))
		}
	}
	if len(frames) != 13 { // 2048 / 160 = 12 full frames + 1 of 128
		t.Fatalf("frame count = %d, want 13", len(frames))
	}
	// Monotonic seq + preserved payload type, starting from the source packet.
	reassembled := make([]byte, 0, total)
	for i, f := range frames {
		if f.SequenceNumber != uint16(7000+i) {
			t.Fatalf("frame %d seq = %d, want %d", i, f.SequenceNumber, 7000+i)
		}
		if f.PayloadType != 0 {
			t.Fatalf("frame %d PT = %d, want 0 (PCMU preserved)", i, f.PayloadType)
		}
		reassembled = append(reassembled, f.Payload...)
	}
	// No bytes lost or reordered across the split.
	for i := range big {
		if reassembled[i] != big[i] {
			t.Fatalf("reassembled payload differs at byte %d", i)
		}
	}
}

func TestRTPBridge_StopTerminatesGoroutines(t *testing.T) {
	a := newFakeMediaSession()
	b := newFakeMediaSession()
	br := NewRTPBridge(a, b, nil)
	br.Start(context.Background())

	done := make(chan struct{})
	go func() { br.Stop(); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return: relay goroutines were not unblocked/terminated")
	}

	// Stop is idempotent.
	br.Stop()
}
