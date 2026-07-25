package conversation_usecase

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/pion/rtp"

	"vozko/domain/sip_trunk"
	"vozko/domain/voip"
	"vozko/infra/voip/audio"
)

type captureMediaSession struct {
	mu        sync.Mutex
	packets   []*rtp.Packet
	writeErr  error
	calls     atomic.Uint64
	codecName string // negotiated codec name; "" => µ-law
}

type noWaitPacer struct{}

func (noWaitPacer) Wait(context.Context, int) error { return nil }

type captureFramePacer struct {
	mu      sync.Mutex
	samples []int
	err     error
}

func (p *captureFramePacer) Wait(_ context.Context, samples int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.samples = append(p.samples, samples)
	return p.err
}

func (p *captureFramePacer) snapshot() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]int, len(p.samples))
	copy(out, p.samples)
	return out
}

func (c *captureMediaSession) ReadRTP(_ []byte, _ interface{}) (int, error) {
	return 0, nil
}

func (c *captureMediaSession) WriteRTP(packet interface{}) error {
	c.calls.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writeErr != nil {
		return c.writeErr
	}
	p, ok := packet.(*rtp.Packet)
	if !ok {
		return errors.New("captureMediaSession: expected *rtp.Packet")
	}

	payload := make([]byte, len(p.Payload))
	copy(payload, p.Payload)
	clone := &rtp.Packet{
		Header:  p.Header,
		Payload: payload,
	}
	c.packets = append(c.packets, clone)
	return nil
}

func (c *captureMediaSession) LocalAddr() net.Addr       { return nil }
func (c *captureMediaSession) RemoteAddr() net.Addr      { return nil }
func (c *captureMediaSession) Close() error              { return nil }
func (c *captureMediaSession) OnDTMF(_ voip.DTMFHandler) {}
func (c *captureMediaSession) UnblockReaders() error     { return nil }
func (c *captureMediaSession) NegotiatedCodec() voip.CodecInfo {
	return voip.CodecInfo{Name: c.codecName}
}

func (c *captureMediaSession) snapshot() []*rtp.Packet {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*rtp.Packet, len(c.packets))
	copy(out, c.packets)
	return out
}

func newTestCall(media *captureMediaSession) *sipTrunkCRMCall {
	return &sipTrunkCRMCall{
		mediaReady: true,
		ssrc:       0xDEADBEEF,
		writePacer: noWaitPacer{},
		session: sip_trunk.TrunkCallSession{
			MediaSession: media,
		},
	}
}

func makePCM(nSamples int) []byte {
	return make([]byte, nSamples*2)
}

func TestSendAudio_BufferPreservesRemainderAcrossChunks(t *testing.T) {
	media := &captureMediaSession{}
	c := newTestCall(media)

	const chunks = 10
	const samplesPerChunk = 1024
	pcm := makePCM(samplesPerChunk)

	for i := 0; i < chunks; i++ {
		if err := c.SendAudio(pcm); err != nil {
			t.Fatalf("SendAudio call %d returned error: %v", i, err)
		}
	}

	pkts := media.snapshot()

	const wantFrames = 64
	if len(pkts) != wantFrames {
		t.Fatalf("frame count = %d, want %d (loss = %d bytes)",
			len(pkts), wantFrames, (wantFrames-len(pkts))*trunkFrameBytes)
	}

	if got := len(c.pendingEncoded); got != 0 {
		t.Errorf("pendingEncoded len after aligned stream = %d, want 0", got)
	}

	for i, p := range pkts {
		if got, want := p.SequenceNumber, uint16(i); got != want {
			t.Fatalf("packet[%d] seq = %d, want %d", i, got, want)
		}
		if got, want := p.Timestamp, uint32(i)*trunkFrameSamples; got != want {
			t.Fatalf("packet[%d] ts = %d, want %d", i, got, want)
		}
		if len(p.Payload) != trunkFrameBytes {
			t.Fatalf("packet[%d] payload size = %d, want %d",
				i, len(p.Payload), trunkFrameBytes)
		}
	}
}

func TestSendAudio_NonAlignedChunkLeavesPending(t *testing.T) {
	media := &captureMediaSession{}
	c := newTestCall(media)

	if err := c.SendAudio(makePCM(1024)); err != nil {
		t.Fatalf("SendAudio: %v", err)
	}
	if got := len(media.snapshot()); got != 6 {
		t.Fatalf("frames after first chunk = %d, want 6", got)
	}
	if got := len(c.pendingEncoded); got != 64 {
		t.Fatalf("pendingEncoded len = %d, want 64", got)
	}
}

func TestSendAudio_PacesEveryCompleteRTPFrame(t *testing.T) {
	media := &captureMediaSession{}
	c := newTestCall(media)
	pacer := &captureFramePacer{}
	c.writePacer = pacer

	if err := c.SendAudio(makePCM(320)); err != nil {
		t.Fatalf("SendAudio: %v", err)
	}

	got := pacer.snapshot()
	if len(got) != 2 {
		t.Fatalf("pacer calls = %d, want 2", len(got))
	}
	for i, samples := range got {
		if samples != trunkFrameSamples {
			t.Fatalf("pacer call %d samples = %d, want %d", i, samples, trunkFrameSamples)
		}
	}
	if gotFrames := len(media.snapshot()); gotFrames != 2 {
		t.Fatalf("frames = %d, want 2", gotFrames)
	}
}

func TestSendAudio_EmptyInputIsNoOp(t *testing.T) {
	media := &captureMediaSession{}
	c := newTestCall(media)

	if err := c.SendAudio(nil); err != nil {
		t.Fatalf("SendAudio(nil): %v", err)
	}
	if err := c.SendAudio([]byte{}); err != nil {
		t.Fatalf("SendAudio(empty): %v", err)
	}
	if got := len(media.snapshot()); got != 0 {
		t.Fatalf("frames after empty input = %d, want 0", got)
	}
}

func TestSendAudio_PendingCapBoundsMemory(t *testing.T) {
	media := &captureMediaSession{
		writeErr: errors.New("simulated RTP failure"),
	}
	c := newTestCall(media)

	for i := 0; i < 20; i++ {

		_ = c.SendAudio(makePCM(1024))
		if got := len(c.pendingEncoded); got > sipTrunkMaxPendingEncoded {
			t.Fatalf("iteration %d: pendingEncoded len %d exceeded cap %d",
				i, got, sipTrunkMaxPendingEncoded)
		}
	}

	if got := len(c.pendingEncoded); got != sipTrunkMaxPendingEncoded {
		t.Fatalf("final pendingEncoded len = %d, want exactly cap %d",
			got, sipTrunkMaxPendingEncoded)
	}
}

func TestSendAudio_WriteErrorPreservesUnsentBytes(t *testing.T) {
	media := &captureMediaSession{writeErr: errors.New("RTP down")}
	c := newTestCall(media)

	err := c.SendAudio(makePCM(320))
	if err == nil {
		t.Fatalf("SendAudio expected error, got nil")
	}
	if got := len(c.pendingEncoded); got != 320 {
		t.Fatalf("pendingEncoded after failed write = %d, want 320", got)
	}

	media.mu.Lock()
	media.writeErr = nil
	media.mu.Unlock()

	if err := c.SendAudio(nil); err != nil {
		t.Fatalf("recovery SendAudio: %v", err)
	}
	if got := len(media.snapshot()); got != 2 {
		t.Fatalf("frames after recovery = %d, want 2", got)
	}
	if got := len(c.pendingEncoded); got != 0 {
		t.Fatalf("pendingEncoded after recovery = %d, want 0", got)
	}
}

func TestSendAudio_EncodesNegotiatedCodec(t *testing.T) {
	// When the trunk negotiated PCMA, the CRM channel must emit A-law on PT 8.
	media := &captureMediaSession{codecName: "PCMA"}
	c := newTestCall(media)
	// dialAndBridge resolves this from the session at mediaReady; mirror it here.
	c.codec = audio.G711CodecForName(media.NegotiatedCodec().Name)

	pcm := makePCM(trunkFrameSamples) // one 20 ms frame
	if err := c.SendAudio(pcm); err != nil {
		t.Fatalf("SendAudio: %v", err)
	}
	pkts := media.snapshot()
	if len(pkts) == 0 {
		t.Fatal("no packets emitted")
	}
	if pkts[0].PayloadType != audio.PayloadTypePCMA {
		t.Fatalf("PayloadType = %d, want %d (PCMA)", pkts[0].PayloadType, audio.PayloadTypePCMA)
	}
	if !bytesEqualConv(pkts[0].Payload, audio.CodecAlaw.Encode(pcm)) {
		t.Fatal("payload is not A-law encoded")
	}
}

func bytesEqualConv(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSendAudio_NotReadyDropsInput(t *testing.T) {
	media := &captureMediaSession{}
	c := newTestCall(media)
	c.mediaReady = false

	if err := c.SendAudio(makePCM(1024)); err != nil {
		t.Fatalf("SendAudio: %v", err)
	}
	if got := media.calls.Load(); got != 0 {
		t.Fatalf("WriteRTP called %d times, want 0", got)
	}
	if got := len(c.pendingEncoded); got != 0 {
		t.Fatalf("pendingEncoded len = %d, want 0", got)
	}
}
