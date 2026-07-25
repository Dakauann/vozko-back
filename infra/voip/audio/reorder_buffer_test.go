package audio

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"

	"vozko/domain/voip"
)

type mockMediaSession struct {
	mu          sync.Mutex
	packets     []mockRTP
	readCh      chan mockRTP
	readErr     error
	errCh       chan error
	closeCh     chan struct{}
	closed      atomic.Bool
	onDTMF      voip.DTMFHandler
	writtenPkts []*rtp.Packet
	writtenMu   sync.Mutex
	unblockCh   chan struct{}
}

// timeoutErr is a net.Error reporting Timeout()==true, mimicking the read-deadline
// timeout mediaSessionAdapter.UnblockReaders triggers on the raw UDP socket.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

type mockRTP struct {
	seq     uint16
	ts      uint32
	ssrc    uint32
	pt      uint8
	payload []byte
}

func newMockMediaSession() *mockMediaSession {
	return &mockMediaSession{
		readCh:    make(chan mockRTP, 64),
		errCh:     make(chan error, 8),
		closeCh:   make(chan struct{}),
		unblockCh: make(chan struct{}, 1),
	}
}

func (m *mockMediaSession) sendPacket(seq uint16, ts uint32, ssrc uint32, pt uint8, payload []byte) {
	p := make([]byte, len(payload))
	copy(p, payload)
	m.readCh <- mockRTP{seq: seq, ts: ts, ssrc: ssrc, pt: pt, payload: p}
}

// sendReadError makes the next ReadRTP return err once (used to inject a transient
// read-deadline timeout, simulating UnblockReaders waking the raw socket reader).
func (m *mockMediaSession) sendReadError(err error) {
	m.errCh <- err
}

func (m *mockMediaSession) ReadRTP(buf []byte, packet interface{}) (int, error) {
	pkt, ok := packet.(*rtp.Packet)
	if !ok {
		return 0, errors.New("not an *rtp.Packet")
	}
	if m.readErr != nil {
		return 0, m.readErr
	}
	select {
	case err := <-m.errCh:
		return 0, err
	case mp, ok := <-m.readCh:
		if !ok {
			return 0, errors.New("read channel closed")
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
	case <-m.closeCh:
		return 0, net.ErrClosed
	case <-m.unblockCh:
		return 0, errors.New("unblocked")
	}
}

func (m *mockMediaSession) WriteRTP(packet interface{}) error {
	pkt, ok := packet.(*rtp.Packet)
	if !ok {
		return errors.New("not an *rtp.Packet")
	}
	m.writtenMu.Lock()
	defer m.writtenMu.Unlock()
	m.writtenPkts = append(m.writtenPkts, pkt)
	return nil
}

func (m *mockMediaSession) LocalAddr() net.Addr  { return &net.UDPAddr{Port: 10000} }
func (m *mockMediaSession) RemoteAddr() net.Addr { return &net.UDPAddr{Port: 20000} }
func (m *mockMediaSession) NegotiatedCodec() voip.CodecInfo {
	return voip.CodecInfo{Name: "PCMU", PayloadType: 0}
}
func (m *mockMediaSession) Close() error {
	if m.closed.CompareAndSwap(false, true) {
		close(m.closeCh)
	}
	return nil
}

func (m *mockMediaSession) OnDTMF(handler voip.DTMFHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onDTMF = handler
}

func (m *mockMediaSession) UnblockReaders() error {
	select {
	case m.unblockCh <- struct{}{}:
	default:
	}
	return nil
}

func TestReorderBuffer_InOrderDelivery(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 3, MaxWait: 40 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rb.Run(ctx)
	defer rb.Close()

	go func() {
		for seq := uint16(100); seq < 110; seq++ {
			mock.sendPacket(seq, uint32(seq-100)*160, 12345, 0, make([]byte, 160))
		}
	}()

	buf := make([]byte, 1500)
	for i := 0; i < 10; i++ {
		pkt := &rtp.Packet{}
		n, err := rb.ReadRTP(buf, pkt)
		if err != nil {
			t.Fatalf("ReadRTP %d failed: %v", i, err)
		}
		if n != 160 {
			t.Errorf("expected payload size 160, got %d", n)
		}
		expectedSeq := uint16(100 + i)
		if pkt.SequenceNumber != expectedSeq {
			t.Errorf("expected seq %d, got %d", expectedSeq, pkt.SequenceNumber)
		}
	}
}

func TestReorderBuffer_ReorderOutOfOrder(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 3, MaxWait: 50 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rb.Run(ctx)
	defer rb.Close()

	go func() {
		mock.sendPacket(100, 0, 12345, 0, make([]byte, 160))
		mock.sendPacket(102, 320, 12345, 0, make([]byte, 160))
		mock.sendPacket(101, 160, 12345, 0, make([]byte, 160))
		for seq := uint16(103); seq < 108; seq++ {
			mock.sendPacket(seq, uint32(seq-100)*160, 12345, 0, make([]byte, 160))
		}
	}()

	buf := make([]byte, 1500)
	for i := 0; i < 8; i++ {
		pkt := &rtp.Packet{}
		_, err := rb.ReadRTP(buf, pkt)
		if err != nil {
			t.Fatalf("ReadRTP %d failed: %v", i, err)
		}
		expected := uint16(100 + i)
		if pkt.SequenceNumber != expected {
			t.Errorf("packet %d: expected seq %d, got %d", i, expected, pkt.SequenceNumber)
		}
	}
}

func TestReorderBuffer_SilenceFillOnLoss(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{
		Depth:   2,
		MaxWait: 30 * time.Millisecond,
		Logger:  t.Logf,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rb.Run(ctx)
	defer rb.Close()

	go func() {
		mock.sendPacket(100, 0, 12345, 0, []byte{0x10})
		mock.sendPacket(103, 480, 12345, 0, []byte{0x13})
	}()

	buf := make([]byte, 1500)

	pkt := &rtp.Packet{}
	if _, err := rb.ReadRTP(buf, pkt); err != nil {
		t.Fatalf("first ReadRTP failed: %v", err)
	}
	if pkt.SequenceNumber != 100 {
		t.Fatalf("expected seq 100, got %d", pkt.SequenceNumber)
	}

	deliveredSeqs := []uint16{100}
	for i := 0; i < 3; i++ {
		pkt := &rtp.Packet{}
		if _, err := rb.ReadRTP(buf, pkt); err != nil {
			t.Fatalf("ReadRTP for seq %d failed: %v", 101+i, err)
		}
		deliveredSeqs = append(deliveredSeqs, pkt.SequenceNumber)
	}

	for i, seq := range deliveredSeqs {
		expected := uint16(100 + i)
		if seq != expected {
			t.Errorf("packet %d: expected seq %d, got %d", i, expected, seq)
		}
	}

	stats := rb.Stats()
	if stats.SilenceFilled < 2 {
		t.Errorf("expected at least 2 silence-filled packets, got %d", stats.SilenceFilled)
	}
}

func TestReorderBuffer_LatePacketDiscarded(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 3, MaxWait: 50 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rb.Run(ctx)
	defer rb.Close()

	go func() {
		mock.sendPacket(100, 0, 12345, 0, []byte{0xAA})
		mock.sendPacket(102, 320, 12345, 0, []byte{0xCC})
		mock.sendPacket(101, 160, 12345, 0, []byte{0xBB})
		for seq := uint16(103); seq < 106; seq++ {
			mock.sendPacket(seq, uint32(seq-100)*160, 12345, 0, []byte{byte(seq)})
		}

		time.Sleep(100 * time.Millisecond)
		mock.sendPacket(100, 0, 12345, 0, []byte{0xDD})
	}()

	buf := make([]byte, 1500)
	var seqs []uint16
	for i := 0; i < 6; i++ {
		pkt := &rtp.Packet{}
		if _, err := rb.ReadRTP(buf, pkt); err != nil {
			t.Fatalf("ReadRTP %d failed: %v", i, err)
		}
		seqs = append(seqs, pkt.SequenceNumber)
	}

	for i, seq := range seqs {
		if seq != uint16(100+i) {
			t.Errorf("packet %d: expected seq %d, got %d", i, 100+i, seq)
		}
	}

	stats := rb.Stats()
	_ = stats
}

func TestReorderBuffer_PassThroughWriteRTP(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 3, MaxWait: 50 * time.Millisecond})

	pkt := &rtp.Packet{
		Header:  rtp.Header{SequenceNumber: 42},
		Payload: []byte{0xFF},
	}
	if err := rb.WriteRTP(pkt); err != nil {
		t.Fatalf("WriteRTP failed: %v", err)
	}

	mock.writtenMu.Lock()
	defer mock.writtenMu.Unlock()
	if len(mock.writtenPkts) != 1 {
		t.Fatalf("expected 1 written packet, got %d", len(mock.writtenPkts))
	}
	if mock.writtenPkts[0].SequenceNumber != 42 {
		t.Errorf("expected seq 42, got %d", mock.writtenPkts[0].SequenceNumber)
	}
}

func TestReorderBuffer_CloseStopsReadLoop(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 3, MaxWait: 50 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rb.Run(ctx)

	time.Sleep(50 * time.Millisecond)
	rb.Close()

	buf := make([]byte, 1500)
	pkt := &rtp.Packet{}
	_, err := rb.ReadRTP(buf, pkt)
	if err == nil {
		t.Error("expected error after Close, got nil")
	}
}

func TestReorderBuffer_PCMAAlawSilenceFill(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{
		Depth:   2,
		MaxWait: 30 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rb.Run(ctx)
	defer rb.Close()

	go func() {
		mock.sendPacket(100, 0, 12345, 8, []byte{0x10})
		mock.sendPacket(103, 480, 12345, 8, []byte{0x13})
	}()

	buf := make([]byte, 1500)

	pkt := &rtp.Packet{}
	if _, err := rb.ReadRTP(buf, pkt); err != nil {
		t.Fatalf("first ReadRTP failed: %v", err)
	}
	if pkt.SequenceNumber != 100 {
		t.Fatalf("expected seq 100, got %d", pkt.SequenceNumber)
	}

	for i := 0; i < 3; i++ {
		pkt := &rtp.Packet{}
		if _, err := rb.ReadRTP(buf, pkt); err != nil {
			t.Fatalf("ReadRTP %d failed: %v", i, err)
		}
		if i < 2 && pkt.PayloadType != 8 {
			t.Errorf("expected PT 8 (PCMA) for silence fill, got %d", pkt.PayloadType)
		}
	}

	stats := rb.Stats()
	if stats.SilenceFilled < 2 {
		t.Errorf("expected at least 2 silence-filled packets for PCMA, got %d", stats.SilenceFilled)
	}
}

func TestReorderBuffer_SequenceWraparound(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 3, MaxWait: 50 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rb.Run(ctx)
	defer rb.Close()

	go func() {
		mock.sendPacket(65534, 0, 12345, 0, []byte{0xAA})
		mock.sendPacket(65535, 160, 12345, 0, []byte{0xBB})
		mock.sendPacket(0, 320, 12345, 0, []byte{0xCC})
		mock.sendPacket(1, 480, 12345, 0, []byte{0xDD})

		for seq := uint16(2); seq < 5; seq++ {
			mock.sendPacket(seq, uint32(seq)*160, 12345, 0, []byte{byte(seq)})
		}
	}()

	buf := make([]byte, 1500)
	expectedSeqs := []uint16{65534, 65535, 0, 1, 2, 3, 4}
	for i, expectedSeq := range expectedSeqs {
		pkt := &rtp.Packet{}
		if _, err := rb.ReadRTP(buf, pkt); err != nil {
			t.Fatalf("ReadRTP %d failed: %v", i, err)
		}
		if pkt.SequenceNumber != expectedSeq {
			t.Errorf("packet %d: expected seq %d, got %d", i, expectedSeq, pkt.SequenceNumber)
		}
	}
}

func TestReorderBuffer_DelegatesPassthrough(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 3, MaxWait: 50 * time.Millisecond})

	if rb.LocalAddr().(*net.UDPAddr).Port != 10000 {
		t.Error("LocalAddr should delegate to inner")
	}
	if rb.RemoteAddr().(*net.UDPAddr).Port != 20000 {
		t.Error("RemoteAddr should delegate to inner")
	}

	var dtmfCalled bool
	rb.OnDTMF(func(r rune) { dtmfCalled = true })
	mock.mu.Lock()
	handler := mock.onDTMF
	mock.mu.Unlock()
	if handler != nil {
		handler('5')
		if !dtmfCalled {
			t.Error("OnDTMF handler not forwarded")
		}
	}
}

func TestReorderBuffer_ContextCancellation(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 3, MaxWait: 50 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	rb.Run(ctx)

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	time.Sleep(100 * time.Millisecond)

	buf := make([]byte, 1500)
	pkt := &rtp.Packet{}
	_, err := rb.ReadRTP(buf, pkt)

	if err == nil {
		t.Error("expected error after context cancellation")
	}
}

func TestReorderBuffer_LargeGapFillsImmediately(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{
		Depth:   3,
		MaxWait: 50 * time.Millisecond,
		Logger:  t.Logf,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rb.Run(ctx)
	defer rb.Close()

	go func() {
		mock.sendPacket(100, 0, 12345, 0, []byte{0x01})
		mock.sendPacket(110, 1600, 12345, 0, []byte{0x0B})
	}()

	buf := make([]byte, 1500)

	pkt := &rtp.Packet{}
	if _, err := rb.ReadRTP(buf, pkt); err != nil {
		t.Fatalf("first ReadRTP failed: %v", err)
	}
	if pkt.SequenceNumber != 100 {
		t.Fatalf("expected seq 100, got %d", pkt.SequenceNumber)
	}

	received := make([]uint16, 0, 11)
	received = append(received, pkt.SequenceNumber)

	for i := 0; i < 10; i++ {
		pkt := &rtp.Packet{}
		if _, err := rb.ReadRTP(buf, pkt); err != nil {
			t.Fatalf("ReadRTP %d failed: %v", i+1, err)
		}
		received = append(received, pkt.SequenceNumber)
	}

	for i, seq := range received {
		if seq != uint16(100+i) {
			t.Errorf("packet %d: expected seq %d, got %d", i, 100+i, seq)
		}
	}

	stats := rb.Stats()
	if stats.SilenceFilled != 9 {
		t.Errorf("expected 9 silence-filled packets, got %d", stats.SilenceFilled)
	}
}

func TestReorderBuffer_StatsTracking(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 3, MaxWait: 30 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rb.Run(ctx)
	defer rb.Close()

	go func() {
		mock.sendPacket(100, 0, 12345, 0, []byte{0xAA})
		mock.sendPacket(102, 320, 12345, 0, []byte{0xCC})
		mock.sendPacket(101, 160, 12345, 0, []byte{0xBB})
		for seq := uint16(103); seq < 106; seq++ {
			mock.sendPacket(seq, uint32(seq-100)*160, 12345, 0, []byte{byte(seq)})
		}
	}()

	buf := make([]byte, 1500)
	for i := 0; i < 6; i++ {
		pkt := &rtp.Packet{}
		if _, err := rb.ReadRTP(buf, pkt); err != nil {
			t.Fatalf("ReadRTP %d failed: %v", i, err)
		}
	}

	stats := rb.Stats()
	t.Logf("Stats: reordered=%d lost=%d late_dropped=%d silence_filled=%d",
		stats.ReorderedPackets, stats.LostPackets, stats.LateDroppedPackets, stats.SilenceFilled)
}

func TestReorderBuffer_LastPayloadRepeat(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{
		Depth:   2,
		MaxWait: 30 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rb.Run(ctx)
	defer rb.Close()

	audioA := []byte{0xAA, 0xBB, 0xCC, 0xDD}
	audioB := []byte{0x11, 0x22, 0x33, 0x44}

	go func() {
		mock.sendPacket(100, 0, 12345, 96, audioA)
		mock.sendPacket(101, 160, 12345, 96, audioB)

		mock.sendPacket(103, 480, 12345, 96, audioA)
	}()

	buf := make([]byte, 1500)

	pkt := &rtp.Packet{}
	if _, err := rb.ReadRTP(buf, pkt); err != nil {
		t.Fatalf("ReadRTP seq 100 failed: %v", err)
	}
	if pkt.SequenceNumber != 100 {
		t.Fatalf("expected seq 100, got %d", pkt.SequenceNumber)
	}

	pkt = &rtp.Packet{}
	if _, err := rb.ReadRTP(buf, pkt); err != nil {
		t.Fatalf("ReadRTP seq 101 failed: %v", err)
	}
	if pkt.SequenceNumber != 101 {
		t.Fatalf("expected seq 101, got %d", pkt.SequenceNumber)
	}

	pkt = &rtp.Packet{}
	if _, err := rb.ReadRTP(buf, pkt); err != nil {
		t.Fatalf("ReadRTP seq 102 (gap fill) failed: %v", err)
	}
	if pkt.SequenceNumber != 102 {
		t.Fatalf("expected seq 102 (gap fill), got %d", pkt.SequenceNumber)
	}
	if pkt.PayloadType != 96 {
		t.Errorf("gap fill should preserve payload type 96, got %d", pkt.PayloadType)
	}
	for i, b := range pkt.Payload {
		if b != audioB[i] {
			t.Errorf("gap fill payload byte %d: expected 0x%02X (last payload repeat), got 0x%02X", i, audioB[i], b)
		}
	}

	pkt = &rtp.Packet{}
	if _, err := rb.ReadRTP(buf, pkt); err != nil {
		t.Fatalf("ReadRTP seq 103 failed: %v", err)
	}
	if pkt.SequenceNumber != 103 {
		t.Fatalf("expected seq 103, got %d", pkt.SequenceNumber)
	}

	stats := rb.Stats()
	if stats.SilenceFilled != 1 {
		t.Errorf("expected 1 silence-filled (last payload repeat), got %d", stats.SilenceFilled)
	}
}

func TestReorderBuffer_DynamicFrameSize(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{
		Depth:   2,
		MaxWait: 30 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rb.Run(ctx)
	defer rb.Close()

	go func() {
		mock.sendPacket(100, 0, 12345, 96, make([]byte, 320))
		mock.sendPacket(101, 960, 12345, 96, make([]byte, 320))

		mock.sendPacket(103, 2880, 12345, 96, make([]byte, 320))
	}()

	buf := make([]byte, 1500)

	for i, expectedSeq := range []uint16{100, 101, 102, 103} {
		pkt := &rtp.Packet{}
		if _, err := rb.ReadRTP(buf, pkt); err != nil {
			t.Fatalf("ReadRTP %d failed: %v", i, err)
		}
		if pkt.SequenceNumber != expectedSeq {
			t.Errorf("packet %d: expected seq %d, got %d", i, expectedSeq, pkt.SequenceNumber)
		}
		if pkt.PayloadType != 96 {
			t.Errorf("packet %d: expected PT 96, got %d", i, pkt.PayloadType)
		}
	}

	stats := rb.Stats()
	if stats.SilenceFilled != 1 {
		t.Errorf("expected 1 gap fill, got %d", stats.SilenceFilled)
	}
}

// TestReorderBuffer_SurvivesTransientReadTimeout is the regression test for the
// inbound-transfer call-drop bug. When an AI→human surrender happens,
// mediaSessionAdapter.UnblockReaders() sets a past read deadline on the raw
// socket to wake the blocked reader, which surfaces as an i/o-timeout net.Error.
// The buffer MUST absorb that transient timeout and keep delivering packets — the
// human's passthrough leg reads from this same buffer. Treating the timeout as
// fatal (the original bug) closed the media out from under the call the human
// just accepted ("audio: reorder buffer closed").
func TestReorderBuffer_SurvivesTransientReadTimeout(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{
		Depth:   3,
		MaxWait: 40 * time.Millisecond,
		Logger:  t.Logf,
		CallID:  "transient-timeout",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rb.Run(ctx)
	defer rb.Close()

	buf := make([]byte, 1500)

	// 1. A packet flows normally before the surrender.
	mock.sendPacket(100, 0, 12345, 0, make([]byte, 160))
	pkt := &rtp.Packet{}
	if _, err := rb.ReadRTP(buf, pkt); err != nil {
		t.Fatalf("pre-surrender ReadRTP failed: %v", err)
	}
	if pkt.SequenceNumber != 100 {
		t.Fatalf("expected seq 100, got %d", pkt.SequenceNumber)
	}

	// 2. The surrender hand-off: UnblockReaders wakes the raw reader with a
	//    read-deadline timeout. Inject a couple to be realistic.
	mock.sendReadError(timeoutErr{})
	mock.sendReadError(timeoutErr{})
	time.Sleep(60 * time.Millisecond) // let readFromInner absorb them

	// 3. The buffer must still be alive and deliver subsequent caller audio to the
	//    human leg. With the bug, this read returns the timeout error or ErrBufferClosed.
	mock.sendPacket(101, 160, 12345, 0, make([]byte, 160))
	pkt2 := &rtp.Packet{}
	if _, err := rb.ReadRTP(buf, pkt2); err != nil {
		t.Fatalf("buffer died after a transient read timeout (UnblockReaders surrender hand-off) — this is the inbound-transfer call-drop bug: %v", err)
	}
	if pkt2.SequenceNumber != 101 {
		t.Fatalf("expected seq 101 after the transient timeout, got %d", pkt2.SequenceNumber)
	}
}

// TestReorderBuffer_NonTimeoutErrorStillClosesBuffer guards the other side: a
// genuine (non-timeout) read error must still tear the buffer down, so a truly
// dead socket is not papered over by the timeout-tolerance.
func TestReorderBuffer_NonTimeoutErrorStillClosesBuffer(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{
		Depth:   3,
		MaxWait: 40 * time.Millisecond,
		Logger:  t.Logf,
		CallID:  "fatal-error",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rb.Run(ctx)
	defer rb.Close()

	buf := make([]byte, 1500)
	mock.sendPacket(200, 0, 1, 0, make([]byte, 160))
	pkt := &rtp.Packet{}
	if _, err := rb.ReadRTP(buf, pkt); err != nil {
		t.Fatalf("first ReadRTP failed: %v", err)
	}

	// A real connection-closed error must propagate, not be swallowed.
	mock.sendReadError(net.ErrClosed)

	pkt2 := &rtp.Packet{}
	if _, err := rb.ReadRTP(buf, pkt2); err == nil {
		t.Fatal("a genuine (non-timeout) read error must close the buffer, but ReadRTP returned no error")
	}
}
