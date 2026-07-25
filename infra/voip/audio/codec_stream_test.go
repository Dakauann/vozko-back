package audio

import (
	"context"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/zaf/g711"
)

// streamWithCodec builds a stream bound to a specific codec and a capturing
// writer that also records the payload type of every emitted packet.
func streamWithCodec(t *testing.T, codec Codec) (*G711RTPStream, *capturingMedia, *[]uint8) {
	t.Helper()
	var pts []uint8
	media := &capturingMedia{hook: func(p *rtp.Packet) { pts = append(pts, p.PayloadType) }}
	s := New(media, Options{
		Codec:      codec,
		SampleRate: DefaultSampleRate,
		FrameDur:   DefaultFrameDur,
		BufferCap:  DefaultBufferCap,
		SSRC:       0xABCD,
	})
	return s, media, &pts
}

func ramp(nSamples int) []byte {
	out := make([]byte, nSamples*2)
	for i := 0; i < nSamples; i++ {
		v := int16((i*411)%4000 - 2000)
		out[i*2] = byte(v)
		out[i*2+1] = byte(v >> 8)
	}
	return out
}

func TestStreamAlawEncodesAndLabelsPT8(t *testing.T) {
	s, media, pts := streamWithCodec(t, CodecAlaw)

	pcm := ramp(DefaultFrameBytes) // 160 samples -> 160 A-law octets per frame
	if err := s.WritePCM16(context.Background(), pcm); err != nil {
		t.Fatalf("WritePCM16: %v", err)
	}
	if err := s.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}

	got := media.snapshot()
	if len(got) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(got))
	}
	if want := g711.EncodeAlaw(pcm); !bytesEqual(got[0].payload, want) {
		t.Fatalf("A-law payload mismatch")
	}
	if (*pts)[0] != PayloadTypePCMA {
		t.Fatalf("payload type = %d, want %d (PCMA)", (*pts)[0], PayloadTypePCMA)
	}
}

func TestStreamAlawSilenceUsesD5(t *testing.T) {
	s, media, pts := streamWithCodec(t, CodecAlaw)

	// No data enqueued -> Step emits a full silence frame.
	if err := s.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}
	got := media.snapshot()
	if len(got) != 1 || len(got[0].payload) != DefaultFrameBytes {
		t.Fatalf("expected one %d-byte silence frame, got %d packets", DefaultFrameBytes, len(got))
	}
	for i, b := range got[0].payload {
		if b != AlawSilence {
			t.Fatalf("silence byte[%d] = %#x, want %#x (A-law)", i, b, AlawSilence)
		}
	}
	if (*pts)[0] != PayloadTypePCMA {
		t.Fatalf("silence payload type = %d, want %d", (*pts)[0], PayloadTypePCMA)
	}
}

func TestStreamDefaultIsMulawUnchanged(t *testing.T) {
	// No Codec set + explicit PayloadType:0 must behave exactly as before.
	media := &capturingMedia{}
	s := New(media, Options{PayloadType: DefaultPayloadType, SampleRate: DefaultSampleRate, FrameDur: DefaultFrameDur})

	pcm := ramp(DefaultFrameBytes)
	if err := s.WritePCM16(context.Background(), pcm); err != nil {
		t.Fatalf("WritePCM16: %v", err)
	}
	if err := s.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}
	got := media.snapshot()
	if want := g711.EncodeUlaw(pcm); !bytesEqual(got[0].payload, want) {
		t.Fatalf("µ-law payload mismatch")
	}
	if s.payloadType != PayloadTypePCMU {
		t.Fatalf("default payload type = %d, want 0", s.payloadType)
	}

	// And a silence frame must be 0xFF.
	media.mu.Lock()
	media.packets = nil
	media.mu.Unlock()
	_ = s.Step()
	sil := media.snapshot()[0].payload
	for i, b := range sil {
		if b != MulawSilence {
			t.Fatalf("µ-law silence byte[%d] = %#x, want %#x", i, b, MulawSilence)
		}
	}
}

func TestStreamWritePCM16Empty(t *testing.T) {
	s := New(&capturingMedia{}, Options{Codec: CodecAlaw, SampleRate: DefaultSampleRate, FrameDur: DefaultFrameDur})
	if err := s.WritePCM16(context.Background(), nil); err != nil {
		t.Fatalf("WritePCM16(nil) = %v, want nil", err)
	}
	if s.BufferedBytes() != 0 {
		t.Fatalf("empty write must enqueue nothing, buffered=%d", s.BufferedBytes())
	}
}

func TestStreamCodecOverridesPayloadType(t *testing.T) {
	// Even if a caller passes a stale PayloadType, the codec wins so the wire
	// label can never disagree with the encoded bytes.
	media := &capturingMedia{}
	s := New(media, Options{PayloadType: 0, Codec: CodecAlaw, SampleRate: DefaultSampleRate, FrameDur: DefaultFrameDur})
	if s.payloadType != PayloadTypePCMA {
		t.Fatalf("payload type = %d, want %d (codec must override)", s.payloadType, PayloadTypePCMA)
	}
	_ = time.Millisecond
}
