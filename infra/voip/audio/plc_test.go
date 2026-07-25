package audio

import (
	"context"
	"testing"
	"time"

	"github.com/pion/rtp"
)

// loudUlaw returns a 160-octet µ-law frame of constant amplitude.
func loudUlaw(amp int16) []byte { return CodecMulaw.Encode(constPCM(160, amp)) }

func TestConcealPayload_FadesLoudRepeat(t *testing.T) {
	rb := &RTPReorderBuffer{observedPT: PayloadTypePCMU, plcDecay: 0.5, frameBytes: 160}
	rb.lastPayload = loudUlaw(10000)

	first := rb.concealPayloadLocked()
	firstPeak := PeakAmplitude(CodecMulaw.Decode(first))
	if firstPeak < 4000 || firstPeak > 6000 {
		t.Fatalf("first concealed peak = %d, want ~5000 (half of 10000)", firstPeak)
	}
	// Simulate the buffer compounding: lastPayload becomes the emitted frame.
	rb.lastPayload = first
	second := rb.concealPayloadLocked()
	secondPeak := PeakAmplitude(CodecMulaw.Decode(second))
	if secondPeak >= firstPeak {
		t.Fatalf("second peak %d should be < first peak %d (fade)", secondPeak, firstPeak)
	}
}

func TestConcealPayload_SnapsToSilenceWhenFaded(t *testing.T) {
	rb := &RTPReorderBuffer{observedPT: PayloadTypePCMA, plcDecay: 0.5, frameBytes: 160}
	// A near-silent last frame (peak below floor) must snap to clean A-law idle.
	rb.lastPayload = CodecAlaw.Encode(constPCM(160, 10))
	got := rb.concealPayloadLocked()
	if len(got) != 160 {
		t.Fatalf("silence frame len = %d, want 160", len(got))
	}
	for i, b := range got {
		if b != AlawSilence {
			t.Fatalf("byte[%d] = %#x, want A-law silence %#x", i, b, AlawSilence)
		}
	}
}

func TestConcealPayload_NoLastFrameEmitsCodecSilence(t *testing.T) {
	rb := &RTPReorderBuffer{observedPT: PayloadTypePCMU, plcDecay: 0.5, frameBytes: 120}
	got := rb.concealPayloadLocked()
	if len(got) != 120 {
		t.Fatalf("len = %d, want 120 (frameBytes)", len(got))
	}
	for _, b := range got {
		if b != MulawSilence {
			t.Fatalf("want µ-law silence %#x", MulawSilence)
		}
	}
}

func TestConcealPayload_UnknownCodecRepeatsThenIdles(t *testing.T) {
	// PT 9 (G722) is not in the media plane → best-effort fallback.
	rb := &RTPReorderBuffer{observedPT: 9, plcDecay: 0.5, frameBytes: 160}
	rb.lastPayload = []byte{1, 2, 3, 4}
	got := rb.concealPayloadLocked()
	if !bytesEqual(got, []byte{1, 2, 3, 4}) {
		t.Fatalf("unknown codec should repeat verbatim, got %v", got)
	}

	rb.lastPayload = nil
	idle := rb.concealPayloadLocked()
	if len(idle) != 160 {
		t.Fatalf("idle len = %d, want 160", len(idle))
	}
	for _, b := range idle {
		if b != MulawSilence {
			t.Fatalf("unknown-codec idle should be µ-law fill")
		}
	}
}

func TestSkipToEarliestLocked(t *testing.T) {
	// Empty buffer → no-op (no panic, nextSeq unchanged).
	rb := &RTPReorderBuffer{packets: map[uint16]*storedPacket{}, nextSeq: 100, started: true}
	rb.skipToEarliestLocked()
	if rb.nextSeq != 100 || rb.stats.Resyncs != 0 {
		t.Fatalf("empty skip should be a no-op, got nextSeq=%d resyncs=%d", rb.nextSeq, rb.stats.Resyncs)
	}

	// Two buffered packets ahead → jump to the nearest (105, not 110).
	rb.packets[110] = &storedPacket{}
	rb.packets[105] = &storedPacket{}
	rb.skipToEarliestLocked()
	if rb.nextSeq != 105 {
		t.Fatalf("nextSeq = %d, want 105 (nearest ahead)", rb.nextSeq)
	}
	if rb.stats.Resyncs != 1 {
		t.Fatalf("Resyncs = %d, want 1", rb.stats.Resyncs)
	}

	// Wrap-aware: nextSeq near the uint16 boundary; nearest-ahead must respect wrap.
	rb2 := &RTPReorderBuffer{packets: map[uint16]*storedPacket{}, nextSeq: 65530, started: true}
	rb2.packets[3] = &storedPacket{}     // forward distance 9 (wraps past 65535)
	rb2.packets[65535] = &storedPacket{} // forward distance 5
	rb2.skipToEarliestLocked()
	if rb2.nextSeq != 65535 {
		t.Fatalf("wrap: nextSeq = %d, want 65535 (distance 5 < 9)", rb2.nextSeq)
	}
}

func TestFrameLenLocked(t *testing.T) {
	if (&RTPReorderBuffer{frameBytes: 0}).frameLenLocked() != initialFrameBytes {
		t.Fatal("frameLenLocked(0) should fall back to initialFrameBytes")
	}
	if (&RTPReorderBuffer{frameBytes: 240}).frameLenLocked() != 240 {
		t.Fatal("frameLenLocked should return frameBytes when set")
	}
}

func TestReorderBuffer_PLCDecayDefaulted(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 1})
	if rb.plcDecay != DefaultPLCDecay {
		t.Fatalf("plcDecay = %v, want default %v", rb.plcDecay, DefaultPLCDecay)
	}
}

// Regression for the production "[reorder] gap timer: filled 65533" bug: an
// oversized sequence jump (huge loss / discontinuity) must RESYNC to the live
// packet, not synthesize tens of thousands of concealment frames.
func TestReorderBuffer_OversizedGapResyncsInsteadOfFlooding(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 3, MaxWait: 30 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rb.Run(ctx)
	defer rb.Close()

	loud := loudUlaw(8000)
	mock.sendPacket(100, 0, 7, PayloadTypePCMU, loud)
	mock.sendPacket(30100, 30000*160, 7, PayloadTypePCMU, loud) // +30000: way past the conceal cap

	buf := make([]byte, 1500)
	p1 := &rtp.Packet{}
	if _, err := rb.ReadRTP(buf, p1); err != nil {
		t.Fatalf("read 1: %v", err)
	}
	if p1.SequenceNumber != 100 {
		t.Fatalf("first seq = %d, want 100", p1.SequenceNumber)
	}
	// Next delivery must be the resync target, NOT 30000 synthetic frames.
	p2 := &rtp.Packet{}
	if _, err := rb.ReadRTP(buf, p2); err != nil {
		t.Fatalf("read 2: %v", err)
	}
	if p2.SequenceNumber != 30100 {
		t.Fatalf("expected resync to seq 30100, got %d (mass-conceal not bounded?)", p2.SequenceNumber)
	}
	stats := rb.Stats()
	if stats.Resyncs == 0 {
		t.Fatal("expected a resync on the oversized gap")
	}
	if stats.SilenceFilled > maxConcealPackets {
		t.Fatalf("SilenceFilled = %d, want ≤ %d (no mass conceal)", stats.SilenceFilled, maxConcealPackets)
	}
}

// A mid-call SSRC change (re-INVITE / re-latch) must resync to the new stream,
// even when the new sequence numbers are unrelated/lower, rather than dropping
// them as "late" forever.
func TestReorderBuffer_SSRCChangeResyncs(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 3, MaxWait: 30 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rb.Run(ctx)
	defer rb.Close()

	loud := loudUlaw(8000)
	mock.sendPacket(100, 0, 0xAAAA, PayloadTypePCMU, loud)
	mock.sendPacket(5, 0, 0xBBBB, PayloadTypePCMU, loud) // new stream: different SSRC, lower seq

	buf := make([]byte, 1500)
	p1 := &rtp.Packet{}
	if _, err := rb.ReadRTP(buf, p1); err != nil {
		t.Fatalf("read 1: %v", err)
	}
	if p1.SequenceNumber != 100 {
		t.Fatalf("first seq = %d, want 100", p1.SequenceNumber)
	}
	p2 := &rtp.Packet{}
	if _, err := rb.ReadRTP(buf, p2); err != nil {
		t.Fatalf("read 2: %v", err)
	}
	if p2.SequenceNumber != 5 || p2.SSRC != 0xBBBB {
		t.Fatalf("expected resync to new stream (seq 5, ssrc 0xBBBB), got seq=%d ssrc=%#x", p2.SequenceNumber, p2.SSRC)
	}
	if rb.Stats().Resyncs == 0 {
		t.Fatal("expected a resync on SSRC change")
	}
}

// Integration: drive a real gap through the buffer and confirm the concealment
// frames it emits fade in amplitude (compounding via lastPayload end-to-end).
func TestReorderBuffer_PLCFadesAcrossGap(t *testing.T) {
	mock := newMockMediaSession()
	rb := NewRTPReorderBuffer(mock, RTPReorderBufferOptions{Depth: 1, MaxWait: 40 * time.Millisecond, PLCDecay: 0.5})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rb.Run(ctx)
	defer rb.Close()

	loud := loudUlaw(12000)
	// seq 100 real, then a jump to seq 105 (Depth=1) → immediate fill of 101..104.
	mock.sendPacket(100, 0, 7, PayloadTypePCMU, loud)
	mock.sendPacket(105, 5*160, 7, PayloadTypePCMU, loud)

	peaks := make([]int, 0, 6)
	buf := make([]byte, 1500)
	for i := 0; i < 6; i++ {
		pkt := &rtp.Packet{}
		n, err := rb.ReadRTP(buf, pkt)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		peaks = append(peaks, PeakAmplitude(CodecMulaw.Decode(pkt.Payload[:n])))
	}

	// peaks[0] real (loud), peaks[1..4] concealed and strictly decreasing,
	// peaks[5] real again (loud).
	if peaks[0] < 8000 {
		t.Fatalf("first real frame peak = %d, expected loud", peaks[0])
	}
	for i := 2; i <= 4; i++ {
		if peaks[i] >= peaks[i-1] {
			t.Fatalf("concealed peaks not fading: %v", peaks)
		}
	}
	if peaks[5] < 8000 {
		t.Fatalf("recovery frame peak = %d, expected loud real audio, peaks=%v", peaks[5], peaks)
	}
}
