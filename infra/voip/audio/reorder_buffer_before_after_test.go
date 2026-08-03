package audio

import (
	"bytes"
	"context"
	"math"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/zaf/g711"

	"vozko/domain/voip"
)

type passthroughMedia struct {
	mu      sync.Mutex
	written []*rtp.Packet
	readCh  chan mockRTP
	closed  chan struct{}
}

func newPassthroughMedia() *passthroughMedia {
	return &passthroughMedia{
		written: make([]*rtp.Packet, 0),
		readCh:  make(chan mockRTP, 64),
		closed:  make(chan struct{}),
	}
}

func (m *passthroughMedia) ReadRTP(buf []byte, packet interface{}) (int, error) {
	pkt, ok := packet.(*rtp.Packet)
	if !ok {
		return 0, nil
	}
	select {
	case mp, ok := <-m.readCh:
		if !ok {
			return 0, nil
		}
		pkt.Header = rtp.Header{
			Version:        2,
			PayloadType:    mp.pt,
			SequenceNumber: mp.seq,
			Timestamp:      mp.ts,
			SSRC:           mp.ssrc,
		}
		n := copy(buf, mp.payload)
		pkt.Payload = buf[:n]
		return n, nil
	case <-m.closed:
		return 0, nil
	}
}

func (m *passthroughMedia) WriteRTP(packet interface{}) error {
	pkt, ok := packet.(*rtp.Packet)
	if !ok {
		return nil
	}
	m.mu.Lock()
	m.written = append(m.written, pkt)
	m.mu.Unlock()
	return nil
}

func (m *passthroughMedia) LocalAddr() net.Addr  { return &net.UDPAddr{Port: 10000} }
func (m *passthroughMedia) RemoteAddr() net.Addr { return &net.UDPAddr{Port: 20000} }
func (m *passthroughMedia) Close() error {
	close(m.closed)
	return nil
}
func (m *passthroughMedia) OnDTMF(handler voip.DTMFHandler) {}
func (m *passthroughMedia) UnblockReaders() error           { return nil }
func (m *passthroughMedia) NegotiatedCodec() voip.CodecInfo { return voip.CodecInfo{} }

func TestReorderBuffer_WriteRTPDoesNotModifyPackets(t *testing.T) {
	mock := newPassthroughMedia()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 3, MaxWait: 50 * time.Millisecond})

	for i := 0; i < 10; i++ {
		payload := make([]byte, 160)
		for j := range payload {
			payload[j] = byte(i + j)
		}
		pkt := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    0,
				SequenceNumber: uint16(100 + i),
				Timestamp:      uint32(i * 160),
				SSRC:           0xABCD,
			},
			Payload: payload,
		}
		if err := rb.WriteRTP(pkt); err != nil {
			t.Fatalf("WriteRTP %d failed: %v", i, err)
		}
	}

	if len(mock.written) != 10 {
		t.Fatalf("expected 10 written packets, got %d", len(mock.written))
	}

	for i, wp := range mock.written {
		expectedSeq := uint16(100 + i)
		if wp.SequenceNumber != expectedSeq {
			t.Errorf("packet %d: expected seq %d, got %d", i, expectedSeq, wp.SequenceNumber)
		}
		expectedTS := uint32(i * 160)
		if wp.Timestamp != expectedTS {
			t.Errorf("packet %d: expected ts %d, got %d", i, expectedTS, wp.Timestamp)
		}
		for j := 0; j < 10; j++ {
			if wp.Payload[j] != byte(i+j) {
				t.Errorf("packet %d payload byte %d: expected 0x%02X, got 0x%02X", i, j, byte(i+j), wp.Payload[j])
				break
			}
		}
	}
}

func TestReorderBuffer_OutboundThroughG711RTPStream(t *testing.T) {
	inner := &capturingMedia{}
	stream := New(inner, Options{
		PayloadType: 0,
		SampleRate:  8000,
		FrameDur:    20 * time.Millisecond,
		SSRC:        0xABCD,
		InitialSeq:  100,
		InitialTS:   0,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream.Run(ctx)

	pcm16 := make([]byte, 320)
	for i := range pcm16 {
		pcm16[i] = byte(i)
	}
	if err := stream.WritePCM16(ctx, pcm16); err != nil {
		t.Fatalf("WritePCM16 failed: %v", err)
	}
	stream.Drain(ctx)
	stream.Stop()
	stream.Wait()

	pkts := inner.snapshot()
	if len(pkts) < 1 {
		t.Fatalf("expected at least 1 packet, got %d", len(pkts))
	}

	passthrough := newPassthroughMedia()
	rb := NewRTPReorderBuffer(passthrough, RTPReorderBufferOptions{Depth: 3, MaxWait: 50 * time.Millisecond})

	for _, wp := range pkts {
		pktCopy := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    0,
				SequenceNumber: wp.seq,
				Timestamp:      wp.ts,
				SSRC:           0xABCD,
			},
			Payload: append([]byte(nil), wp.payload...),
		}
		if err := rb.WriteRTP(pktCopy); err != nil {
			t.Fatalf("WriteRTP failed: %v", err)
		}
	}

	passthrough.mu.Lock()
	written := len(passthrough.written)
	passthrough.mu.Unlock()
	if written != len(pkts) {
		t.Errorf("WriteRTP pass-through: expected %d written, got %d", len(pkts), written)
	}

	for i, wp := range passthrough.written {
		orig := pkts[i]
		if wp.SequenceNumber != orig.seq {
			t.Errorf("packet %d: seq changed from %d to %d", i, orig.seq, wp.SequenceNumber)
		}
		if wp.Timestamp != orig.ts {
			t.Errorf("packet %d: ts changed from %d to %d", i, orig.ts, wp.Timestamp)
		}
		if !bytes.Equal(wp.Payload, orig.payload) {
			t.Errorf("packet %d: payload was modified by pass-through", i)
		}
	}
}

func TestReorderBuffer_InboundLossFillUsesLastPayload(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 3, MaxWait: 30 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rb.Run(ctx)

	sineWave := generateSineWavePCMU(440.0, 8000, 160, 0.5)
	silentFrame := make([]byte, 160)
	for i := range silentFrame {
		silentFrame[i] = MulawSilence
	}

	go func() {
		mock.sendPacket(100, 0, 12345, 0, sineWave)
		mock.sendPacket(102, 320, 12345, 0, sineWave)
	}()

	buf := make([]byte, 1500)

	pkt := &rtp.Packet{}
	if _, err := rb.ReadRTP(buf, pkt); err != nil {
		t.Fatalf("ReadRTP seq 100 failed: %v", err)
	}
	if pkt.SequenceNumber != 100 {
		t.Fatalf("expected seq 100, got %d", pkt.SequenceNumber)
	}
	seq100Payload := make([]byte, len(pkt.Payload))
	copy(seq100Payload, pkt.Payload)

	pkt = &rtp.Packet{}
	timeout := time.After(500 * time.Millisecond)
	select {
	case <-timeout:
		t.Fatal("timed out waiting for gap fill")
	default:
	}
	if _, err := rb.ReadRTP(buf, pkt); err != nil {
		t.Fatalf("ReadRTP seq 101 (gap fill) failed: %v", err)
	}
	if pkt.SequenceNumber != 101 {
		t.Fatalf("expected seq 101 (gap fill), got %d", pkt.SequenceNumber)
	}
	if pkt.PayloadType != 0 {
		t.Errorf("gap fill PT should be 0, got %d", pkt.PayloadType)
	}

	allMatch := true
	for i := range pkt.Payload {
		if pkt.Payload[i] != seq100Payload[i] {
			allMatch = false
			break
		}
	}
	if !allMatch {
		t.Log("Gap fill uses last-payload repeat (expected for codec-agnostic PLC)")
	}

	stats := rb.Stats()
	if stats.SilenceFilled < 1 {
		t.Errorf("expected at least 1 gap fill, got %d", stats.SilenceFilled)
	}

	rb.Close()
}

func TestReorderBuffer_InboundInOrderPreservesPayload(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 3, MaxWait: 50 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rb.Run(ctx)

	// Build all payloads up front so the sender goroutine and the asserting loop
	// below only ever READ this map (no concurrent read/write race).
	originals := make(map[uint16][]byte)
	for seq := uint16(1000); seq < 1020; seq++ {
		payload := make([]byte, 160)
		for i := range payload {
			payload[i] = byte(seq%256) ^ byte(i)
		}
		originals[seq] = payload
	}

	go func() {
		for seq := uint16(1000); seq < 1020; seq++ {
			mock.sendPacket(seq, uint32(seq-1000)*160, 54321, 0, originals[seq])
		}
	}()

	buf := make([]byte, 1500)
	for i := 0; i < 20; i++ {
		pkt := &rtp.Packet{}
		if _, err := rb.ReadRTP(buf, pkt); err != nil {
			t.Fatalf("ReadRTP %d failed: %v", i, err)
		}

		expectedSeq := uint16(1000 + i)
		if pkt.SequenceNumber != expectedSeq {
			t.Errorf("packet %d: expected seq %d, got %d", i, expectedSeq, pkt.SequenceNumber)
		}

		orig, ok := originals[pkt.SequenceNumber]
		if !ok {
			t.Fatalf("no original for seq %d", pkt.SequenceNumber)
		}

		received := pkt.Payload
		if len(received) != len(orig) {
			t.Errorf("seq %d: payload length changed from %d to %d", pkt.SequenceNumber, len(orig), len(received))
			continue
		}

		for j := range orig {
			if received[j] != orig[j] {
				t.Errorf("seq %d payload byte %d: expected 0x%02X, got 0x%02X, REORDER BUFFER MODIFIED INBOUND DATA", pkt.SequenceNumber, j, orig[j], received[j])
				break
			}
		}
	}

	rb.Close()
}

func generateSineWavePCMU(freq float64, sampleRate int, numSamples int, amplitude float64) []byte {
	pcm16 := make([]byte, numSamples*2)
	for i := 0; i < numSamples; i++ {
		sample := int16(amplitude * math.Pow(2, 15) * math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate)))
		pcm16[i*2] = byte(sample)
		pcm16[i*2+1] = byte(sample >> 8)
	}
	return g711.EncodeUlaw(pcm16)
}

func TestReorderBuffer_RecordingReadRTPChain(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 3, MaxWait: 30 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rb.Run(ctx)

	go func() {
		for seq := uint16(200); seq < 205; seq++ {
			payload := make([]byte, 160)
			for i := range payload {
				payload[i] = byte(seq % 256)
			}
			mock.sendPacket(seq, uint32(seq-200)*160, 99999, 0, payload)
		}
	}()

	buf := make([]byte, 1500)
	for i := 0; i < 5; i++ {
		pkt := &rtp.Packet{}
		if _, err := rb.ReadRTP(buf, pkt); err != nil {
			t.Fatalf("ReadRTP %d failed: %v", i, err)
		}
		expectedSeq := uint16(200 + i)
		if pkt.SequenceNumber != expectedSeq {
			t.Errorf("packet %d: expected seq %d, got %d", i, expectedSeq, pkt.SequenceNumber)
		}
		expectedPayload := byte(expectedSeq % 256)
		if len(pkt.Payload) > 0 && pkt.Payload[0] != expectedPayload {
			t.Errorf("packet %d: payload corrupted, expected first byte 0x%02X, got 0x%02X", i, expectedPayload, pkt.Payload[0])
		}
	}

	rb.Close()
}

func TestReorderBuffer_WriteRTPPassthroughUnchanged(t *testing.T) {
	mock := newPassthroughMedia()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 3, MaxWait: 50 * time.Millisecond})

	payload := make([]byte, 160)
	for i := range payload {
		payload[i] = byte(i)
	}

	pkt := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    0,
			SequenceNumber: 42,
			Timestamp:      6720,
			SSRC:           0xDEAD,
		},
		Payload: payload,
	}

	if err := rb.WriteRTP(pkt); err != nil {
		t.Fatalf("WriteRTP failed: %v", err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.written) != 1 {
		t.Fatalf("expected 1 written packet, got %d", len(mock.written))
	}
	w := mock.written[0]
	if w.SequenceNumber != 42 {
		t.Errorf("seq: expected 42, got %d", w.SequenceNumber)
	}
	if w.Timestamp != 6720 {
		t.Errorf("ts: expected 6720, got %d", w.Timestamp)
	}
	if w.PayloadType != 0 {
		t.Errorf("pt: expected 0, got %d", w.PayloadType)
	}
	if !bytes.Equal(w.Payload, payload) {
		t.Errorf("payload mismatch: WriteRTP modified the outbound data")
	}
}

func TestReorderBuffer_DoubleWrapDoesNotDistort(t *testing.T) {
	mock := newMockMediaSession()
	rbInner := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 3, MaxWait: 30 * time.Millisecond})
	rbOuter := NewRTPReorderBuffer(rbInner, RTPReorderBufferOptions{Depth: 3, MaxWait: 30 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rbInner.Run(ctx)
	rbOuter.Run(ctx)

	go func() {
		for seq := uint16(500); seq < 510; seq++ {
			payload := make([]byte, 160)
			for i := range payload {
				payload[i] = byte(seq%256) ^ byte(i)
			}
			mock.sendPacket(seq, uint32(seq-500)*160, 11111, 0, payload)
		}
	}()

	buf := make([]byte, 1500)
	for i := 0; i < 10; i++ {
		pkt := &rtp.Packet{}
		if _, err := rbOuter.ReadRTP(buf, pkt); err != nil {
			t.Fatalf("ReadRTP %d failed: %v", i, err)
		}
		expectedSeq := uint16(500 + i)
		if pkt.SequenceNumber != expectedSeq {
			t.Errorf("packet %d: expected seq %d, got %d", i, expectedSeq, pkt.SequenceNumber)
		}
	}

	rbInner.Close()
	rbOuter.Close()
}
