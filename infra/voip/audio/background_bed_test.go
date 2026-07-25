package audio

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// A bed level comfortably above the µ-law quantization floor so it survives the
// decode → mix → encode round trip and is unambiguously audible in assertions.
const testBedLevel = int16(6000)

func constantBed(n int, level int16) []int16 {
	bed := make([]int16, n)
	for i := range bed {
		bed[i] = level
	}
	return bed
}

func TestBackgroundBed_MixesUnderSilence(t *testing.T) {
	codec := CodecMulaw
	const frameBytes = 160

	b := newBackgroundBed(constantBed(frameBytes, testBedLevel))

	payload := codec.Silence(frameBytes)
	b.mixFrame(payload, codec)

	if bytes.Equal(payload, codec.Silence(frameBytes)) {
		t.Fatal("background bed left a silent frame unchanged; the bed was not mixed in")
	}

	pcm := codec.Decode(payload)
	sample := int16(binary.LittleEndian.Uint16(pcm[frameBytes/2*2:]))
	if sample < testBedLevel/2 {
		t.Fatalf("mixed sample %d is far below the bed level %d; bed too quiet or dropped", sample, testBedLevel)
	}
}

func TestBackgroundBed_NilIsNoOp(t *testing.T) {
	if b := newBackgroundBed(nil); b != nil {
		t.Fatalf("newBackgroundBed(nil) = %v, want nil", b)
	}

	codec := CodecMulaw
	payload := codec.Silence(160)
	var b *backgroundBed // nil receiver
	b.mixFrame(payload, codec)

	if !bytes.Equal(payload, codec.Silence(160)) {
		t.Fatal("nil background bed altered the frame; it must be a pure no-op")
	}
}

func TestBackgroundBed_LoopsContinuously(t *testing.T) {
	codec := CodecMulaw
	const frameBytes = 160

	// Bed shorter than a frame forces multiple wraps within a single frame and
	// across frames, proving the cursor loops seamlessly.
	bedLen := frameBytes/2 + 7
	b := newBackgroundBed(constantBed(bedLen, testBedLevel))

	for frame := 0; frame < 5; frame++ {
		payload := codec.Silence(frameBytes)
		b.mixFrame(payload, codec)
		if bytes.Equal(payload, codec.Silence(frameBytes)) {
			t.Fatalf("frame %d came out silent; the looped bed stopped playing", frame)
		}
	}

	// After 5 frames the cursor must stay within bounds (never runs off the end).
	if b.pos < 0 || b.pos >= bedLen {
		t.Fatalf("loop cursor %d out of range [0,%d)", b.pos, bedLen)
	}
}

func TestBackgroundBed_StreamMixesEveryFrame(t *testing.T) {
	media := &capturingMedia{}
	stream := New(media, Options{
		Codec:         CodecMulaw,
		BackgroundBed: constantBed(DefaultFrameBytes, testBedLevel),
	})

	// Drive frames synchronously through Step (no emit goroutine, no timing).
	const frames = 3
	for i := 0; i < frames; i++ {
		if err := stream.Step(); err != nil {
			t.Fatalf("Step %d: %v", i, err)
		}
	}

	pkts := media.snapshot()
	if len(pkts) != frames {
		t.Fatalf("emitted %d frames, want %d", len(pkts), frames)
	}
	silence := CodecMulaw.Silence(DefaultFrameBytes)
	for i, p := range pkts {
		if bytes.Equal(p.payload, silence) {
			t.Fatalf("frame %d went out as pure silence; the whole-call bed was not applied", i)
		}
	}
}
