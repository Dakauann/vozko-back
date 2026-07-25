package audio

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

type capturingMedia struct {
	mu      sync.Mutex
	packets []capturedPacket
	hook    func(*rtp.Packet)
	err     error
}

type capturedPacket struct {
	seq        uint16
	ts         uint32
	payload    []byte
	receivedAt time.Time
}

func (m *capturingMedia) WriteRTP(p interface{}) error {
	pkt, ok := p.(*rtp.Packet)
	if !ok {
		return errors.New("not an *rtp.Packet")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	cp := capturedPacket{
		seq:        pkt.SequenceNumber,
		ts:         pkt.Timestamp,
		payload:    append([]byte(nil), pkt.Payload...),
		receivedAt: time.Now(),
	}
	m.packets = append(m.packets, cp)
	if m.hook != nil {
		m.hook(pkt)
	}
	return nil
}

func (m *capturingMedia) snapshot() []capturedPacket {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]capturedPacket, len(m.packets))
	copy(out, m.packets)
	return out
}

func (m *capturingMedia) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.packets)
}

var _ RTPMediaWriter = (*capturingMedia)(nil)

type silentMedia struct{ capturingMedia }

func (*silentMedia) ReadRTP(_ []byte, _ interface{}) (int, error) { return 0, nil }
func (*silentMedia) LocalAddr() net.Addr                          { return &net.UDPAddr{} }
func (*silentMedia) RemoteAddr() net.Addr                         { return &net.UDPAddr{} }
func (*silentMedia) Close() error                                 { return nil }
func (*silentMedia) OnDTMF(voip.DTMFHandler)                      {}
func (*silentMedia) UnblockReaders() error                        { return nil }
func (*silentMedia) NegotiatedCodec() voip.CodecInfo              { return voip.CodecInfo{} }

func newStream(media RTPMediaWriter) *G711RTPStream {
	return New(media, Options{
		PayloadType: DefaultPayloadType,
		SampleRate:  DefaultSampleRate,
		FrameDur:    DefaultFrameDur,
		BufferCap:   DefaultBufferCap,
		SSRC:        0xCAFEBABE,
		InitialSeq:  1000,
		InitialTS:   2000,
	})
}

func pcm16Silence(nSamples int) []byte { return make([]byte, nSamples*2) }

func pcm16Tone(nSamples int) []byte {
	buf := make([]byte, nSamples*2)
	for i := 0; i < nSamples; i++ {

		buf[2*i] = 0x00
		buf[2*i+1] = 0x40
	}
	return buf
}

func TestStep_AllSilenceWhenEmpty(t *testing.T) {
	media := &capturingMedia{}
	s := newStream(media)

	if err := s.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}

	pkts := media.snapshot()
	if len(pkts) != 1 {
		t.Fatalf("want 1 packet, got %d", len(pkts))
	}
	if len(pkts[0].payload) != DefaultFrameBytes {
		t.Fatalf("payload=%d, want %d", len(pkts[0].payload), DefaultFrameBytes)
	}
	for i, b := range pkts[0].payload {
		if b != MulawSilence {
			t.Fatalf("byte %d = 0x%02x, want 0xFF", i, b)
		}
	}

	m := s.Metrics()
	if m.SilenceFrames != 1 || m.AudioFrames != 0 || m.PartialFrames != 0 {
		t.Fatalf("metrics: %+v", m)
	}
}

func TestStep_FullAudioFrame(t *testing.T) {
	media := &capturingMedia{}
	s := newStream(media)

	if err := s.WritePCM16(context.Background(), pcm16Tone(160)); err != nil {
		t.Fatalf("WritePCM16: %v", err)
	}
	if err := s.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}

	pkts := media.snapshot()
	if len(pkts) != 1 || len(pkts[0].payload) != DefaultFrameBytes {
		t.Fatalf("packets=%+v", pkts)
	}
	silenceCount := 0
	for _, b := range pkts[0].payload {
		if b == MulawSilence {
			silenceCount++
		}
	}
	if silenceCount == DefaultFrameBytes {
		t.Fatal("all bytes were silence; expected audio payload")
	}

	m := s.Metrics()
	if m.AudioFrames != 1 || m.BytesEmitted != uint64(DefaultFrameBytes) {
		t.Fatalf("metrics: %+v", m)
	}
}

func TestStep_PartialFramePaddedWithSilence(t *testing.T) {
	media := &capturingMedia{}
	s := newStream(media)

	if err := s.WritePCM16(context.Background(), pcm16Tone(50)); err != nil {
		t.Fatalf("WritePCM16: %v", err)
	}
	if got := s.BufferedBytes(); got != 50 {
		t.Fatalf("buffered=%d, want 50", got)
	}
	if err := s.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}

	pkts := media.snapshot()
	if len(pkts) != 1 {
		t.Fatalf("packets=%d", len(pkts))
	}
	payload := pkts[0].payload
	if len(payload) != DefaultFrameBytes {
		t.Fatalf("payload=%d", len(payload))
	}

	for i := 50; i < DefaultFrameBytes; i++ {
		if payload[i] != MulawSilence {
			t.Fatalf("byte %d = 0x%02x, want 0xFF (silence padding)", i, payload[i])
		}
	}
	if s.BufferedBytes() != 0 {
		t.Fatal("buf should be drained")
	}
	m := s.Metrics()
	if m.PartialFrames != 1 {
		t.Fatalf("partial=%d, want 1", m.PartialFrames)
	}
}

func TestStep_SequentialFramesMonotonic(t *testing.T) {
	media := &capturingMedia{}
	s := newStream(media)

	_ = s.Step()
	_ = s.WritePCM16(context.Background(), pcm16Tone(160))
	_ = s.Step()
	_ = s.WritePCM16(context.Background(), pcm16Tone(40))
	_ = s.Step()

	pkts := media.snapshot()
	if len(pkts) != 3 {
		t.Fatalf("packets=%d", len(pkts))
	}
	for i := 1; i < len(pkts); i++ {
		if pkts[i].seq != pkts[i-1].seq+1 {
			t.Fatalf("seq jump: %d→%d", pkts[i-1].seq, pkts[i].seq)
		}
		if d := pkts[i].ts - pkts[i-1].ts; d != uint32(DefaultFrameBytes) {
			t.Fatalf("ts delta=%d, want %d", d, DefaultFrameBytes)
		}
	}
}

func TestWritePCM16_BackPressureBlocksAndCtxUnblocks(t *testing.T) {
	media := &capturingMedia{}
	s := New(media, Options{
		BufferCap: 320,
	})

	if err := s.WritePCM16(context.Background(), pcm16Tone(320)); err != nil {
		t.Fatalf("initial write: %v", err)
	}
	if s.BufferedBytes() != 320 {
		t.Fatalf("buffered=%d", s.BufferedBytes())
	}

	ctx, cancel := context.WithCancel(context.Background())
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- s.WritePCM16(ctx, pcm16Tone(160))
	}()

	select {
	case err := <-writeDone:
		t.Fatalf("write returned too early: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-writeDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("write did not unblock after ctx cancel")
	}

	if m := s.Metrics(); m.Overflows == 0 {
		t.Fatal("expected overflows metric to be nonzero")
	}
}

func TestWritePCM16_BackPressureResumesAfterStep(t *testing.T) {
	media := &capturingMedia{}
	s := New(media, Options{BufferCap: 320})

	_ = s.WritePCM16(context.Background(), pcm16Tone(320))

	writeDone := make(chan error, 1)
	go func() {
		writeDone <- s.WritePCM16(context.Background(), pcm16Tone(160))
	}()

	time.Sleep(20 * time.Millisecond)
	_ = s.Step()

	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("write err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("write did not unblock after Step")
	}
}

func TestDrain_WaitsUntilBufferEmpty(t *testing.T) {
	media := &capturingMedia{}
	s := newStream(media)

	if err := s.WritePCM16(context.Background(), pcm16Tone(2*160+30)); err != nil {
		t.Fatalf("write: %v", err)
	}

	drainErr := make(chan error, 1)
	go func() { drainErr <- s.Drain(context.Background()) }()

	select {
	case err := <-drainErr:
		t.Fatalf("Drain returned early: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	for i := 0; i < 3; i++ {
		_ = s.Step()
	}

	select {
	case err := <-drainErr:
		if err != nil {
			t.Fatalf("Drain err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Drain did not return after buffer emptied")
	}

	if s.BufferedBytes() != 0 {
		t.Fatal("buffer should be empty after Drain")
	}
}

func TestDrain_CtxCancel(t *testing.T) {
	s := newStream(&capturingMedia{})
	_ = s.WritePCM16(context.Background(), pcm16Tone(160))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Drain(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
}

func TestStop_ClosesStream(t *testing.T) {
	media := &capturingMedia{}
	s := newStream(media)
	s.Run(context.Background())
	time.Sleep(30 * time.Millisecond)
	s.Stop()
	s.Wait()

	if err := s.WritePCM16(context.Background(), pcm16Tone(10)); !errors.Is(err, ErrStreamClosed) {
		t.Fatalf("WritePCM16 post-Stop err=%v, want ErrStreamClosed", err)
	}
}

func TestRun_EmitsAt20msCadenceWithSilenceFill(t *testing.T) {
	if testing.Short() {
		t.Skip("uses real time")
	}
	media := &capturingMedia{}
	s := newStream(media)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Run(ctx)

	for i := 0; i < 3; i++ {
		if err := s.WritePCM16(context.Background(), pcm16Tone(400)); err != nil {
			t.Fatalf("WritePCM16: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
	}

	s.Stop()
	s.Wait()

	pkts := media.snapshot()
	if len(pkts) < 30 {
		t.Fatalf("expected >=30 packets (~900ms / 20ms), got %d", len(pkts))
	}

	for i, p := range pkts {
		if len(p.payload) != DefaultFrameBytes {
			t.Fatalf("packet %d payload=%d, want %d", i, len(p.payload), DefaultFrameBytes)
		}
	}

	for i := 1; i < len(pkts); i++ {
		if pkts[i].seq != pkts[i-1].seq+1 {
			t.Fatalf("seq jump at %d: %d→%d", i, pkts[i-1].seq, pkts[i].seq)
		}
		if d := pkts[i].ts - pkts[i-1].ts; d != uint32(DefaultFrameBytes) {
			t.Fatalf("ts delta at %d = %d, want %d", i, d, DefaultFrameBytes)
		}
	}

	deltas := make([]time.Duration, 0, len(pkts)-1)
	for i := 1; i < len(pkts); i++ {
		deltas = append(deltas, pkts[i].receivedAt.Sub(pkts[i-1].receivedAt))
	}
	for i, d := range deltas {
		if d < 5*time.Millisecond || d > 60*time.Millisecond {
			t.Fatalf("packet %d arrived %v after prev (want ~20ms ±drift)", i+1, d)
		}
	}

	m := s.Metrics()
	if m.SilenceFrames == 0 {
		t.Fatal("no silence frames emitted; underrun-fill did not engage")
	}
	if m.AudioFrames+m.PartialFrames == 0 {
		t.Fatal("no audio frames emitted; producer was never drained")
	}
}

func TestRun_BurstyTTSDeliveryNeverChopsAudio(t *testing.T) {
	media := &capturingMedia{}
	s := newStream(media)
	ctx := context.Background()

	tagPCM := func(amp int16, nSamples int) []byte {
		buf := make([]byte, nSamples*2)
		for i := 0; i < nSamples; i++ {
			buf[2*i] = byte(amp & 0xff)
			buf[2*i+1] = byte((amp >> 8) & 0xff)
		}
		return buf
	}

	_ = s.WriteEncoded(ctx, tagUlaw(250, 0x55))
	_ = s.Step()
	_ = s.Step()

	for i := 0; i < 5; i++ {
		_ = s.Step()
	}

	_ = s.WriteEncoded(ctx, tagUlaw(480, 0xAA))
	for i := 0; i < 3; i++ {
		_ = s.Step()
	}

	for i := 0; i < 3; i++ {
		_ = s.Step()
	}

	_ = s.WriteEncoded(ctx, tagUlaw(50, 0x33))
	_ = s.Step()

	_ = tagPCM

	pkts := media.snapshot()
	expected := 2 + 5 + 3 + 3 + 1
	if len(pkts) != expected {
		t.Fatalf("packets=%d, want %d", len(pkts), expected)
	}
	for i, p := range pkts {
		if len(p.payload) != DefaultFrameBytes {
			t.Fatalf("packet %d payload=%d, want %d", i, len(p.payload), DefaultFrameBytes)
		}
	}

	for i, b := range pkts[0].payload {
		if b != 0x55 {
			t.Fatalf("chunk1 packet0 byte %d = 0x%02x, want 0x55", i, b)
		}
	}
	for i := 0; i < 90; i++ {
		if pkts[1].payload[i] != 0x55 {
			t.Fatalf("chunk1 packet1 byte %d = 0x%02x, want 0x55", i, pkts[1].payload[i])
		}
	}
	for i := 90; i < DefaultFrameBytes; i++ {
		if pkts[1].payload[i] != MulawSilence {
			t.Fatalf("chunk1 packet1 byte %d = 0x%02x, want 0xFF silence", i, pkts[1].payload[i])
		}
	}

	for idx := 2; idx <= 6; idx++ {
		for i, b := range pkts[idx].payload {
			if b != MulawSilence {
				t.Fatalf("silence packet %d byte %d = 0x%02x, want 0xFF", idx, i, b)
			}
		}
	}

	for idx := 7; idx <= 9; idx++ {
		for i, b := range pkts[idx].payload {
			if b != 0xAA {
				t.Fatalf("chunk2 packet %d byte %d = 0x%02x, want 0xAA", idx, i, b)
			}
		}
	}
}

func tagUlaw(nBytes int, tag byte) []byte {
	buf := make([]byte, nBytes)
	for i := range buf {
		buf[i] = tag
	}
	return buf
}

func TestRun_NoAudioWithSilentStream(t *testing.T) {
	if testing.Short() {
		t.Skip("uses real time")
	}
	media := &capturingMedia{}
	s := newStream(media)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Run(ctx)

	time.Sleep(100 * time.Millisecond)
	s.Stop()
	s.Wait()

	pkts := media.snapshot()
	if len(pkts) < 3 {
		t.Fatalf("expected at least 3 silence packets in 100ms, got %d", len(pkts))
	}
	for i, p := range pkts {
		for j, b := range p.payload {
			if b != MulawSilence {
				t.Fatalf("packet %d byte %d = 0x%02x, want 0xFF silence", i, j, b)
			}
		}
	}
	m := s.Metrics()
	if m.SilenceFrames < 3 {
		t.Fatalf("silence frames metric=%d, want >=3", m.SilenceFrames)
	}
}

func TestRun_WriteRTPErrorsDoNotCrashStream(t *testing.T) {
	if testing.Short() {
		t.Skip("uses real time")
	}
	media := &capturingMedia{err: errors.New("transient write failure")}
	s := newStream(media)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Run(ctx)
	time.Sleep(80 * time.Millisecond)
	s.Stop()
	s.Wait()

	m := s.Metrics()
	if m.WriteRTPErrors == 0 {
		t.Fatal("expected WriteRTPErrors > 0")
	}
	if m.LastWriteRTPErr == nil {
		t.Fatal("expected LastWriteRTPErr to be recorded")
	}
}

func TestStep_NilMediaIsNoop(t *testing.T) {
	s := New(nil, Options{})
	if err := s.Step(); err != nil {
		t.Fatalf("Step nil media err=%v", err)
	}
	if s.Metrics().SilenceFrames != 1 {
		t.Fatalf("metrics=%+v", s.Metrics())
	}
}
