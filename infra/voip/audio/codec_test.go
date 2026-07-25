package audio

import (
	"encoding/binary"
	"testing"

	"github.com/zaf/g711"
)

// pcm16 builds a little-endian PCM16 buffer from sample values.
func pcm16(samples ...int16) []byte {
	out := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(s))
	}
	return out
}

func TestCodecMetadata(t *testing.T) {
	if CodecMulaw.Name() != "PCMU" || CodecMulaw.PayloadType() != PayloadTypePCMU {
		t.Fatalf("µ-law metadata wrong: name=%q pt=%d", CodecMulaw.Name(), CodecMulaw.PayloadType())
	}
	if CodecAlaw.Name() != "PCMA" || CodecAlaw.PayloadType() != PayloadTypePCMA {
		t.Fatalf("A-law metadata wrong: name=%q pt=%d", CodecAlaw.Name(), CodecAlaw.PayloadType())
	}
	if CodecMulaw.SilenceByte() != MulawSilence {
		t.Fatalf("µ-law silence byte = %#x, want %#x", CodecMulaw.SilenceByte(), MulawSilence)
	}
	if CodecAlaw.SilenceByte() != AlawSilence {
		t.Fatalf("A-law silence byte = %#x, want %#x", CodecAlaw.SilenceByte(), AlawSilence)
	}
}

func TestCodecEncodeMatchesG711(t *testing.T) {
	pcm := pcm16(0, 1000, -1000, 32767, -32768, 25, -25)

	if got, want := CodecMulaw.Encode(pcm), g711.EncodeUlaw(pcm); !bytesEqual(got, want) {
		t.Fatalf("µ-law encode mismatch: got %v want %v", got, want)
	}
	if got, want := CodecAlaw.Encode(pcm), g711.EncodeAlaw(pcm); !bytesEqual(got, want) {
		t.Fatalf("A-law encode mismatch: got %v want %v", got, want)
	}
}

func TestCodecDecodeMatchesG711(t *testing.T) {
	// Cover the full octet range for both laws.
	payload := make([]byte, 256)
	for i := range payload {
		payload[i] = byte(i)
	}

	if got, want := CodecMulaw.Decode(payload), g711.DecodeUlaw(payload); !bytesEqual(got, want) {
		t.Fatalf("µ-law decode mismatch")
	}
	if got, want := CodecAlaw.Decode(payload), g711.DecodeAlaw(payload); !bytesEqual(got, want) {
		t.Fatalf("A-law decode mismatch")
	}
}

func TestCodecRoundTripIsStable(t *testing.T) {
	// G.711 is lossy, but decode(encode(x)) must be idempotent under a second
	// encode/decode cycle (the companding lands on a quantization grid).
	for _, c := range []Codec{CodecMulaw, CodecAlaw} {
		pcm := pcm16(0, 4000, -4000, 12000, -12000, 32000, -32000)
		once := c.Decode(c.Encode(pcm))
		twice := c.Decode(c.Encode(once))
		if !bytesEqual(once, twice) {
			t.Fatalf("%s: round trip not stable", c.Name())
		}
	}
}

func TestCodecEncodeDecodeEmpty(t *testing.T) {
	for _, c := range []Codec{CodecMulaw, CodecAlaw} {
		if got := c.Encode(nil); got != nil {
			t.Fatalf("%s: Encode(nil) = %v, want nil", c.Name(), got)
		}
		if got := c.Decode(nil); got != nil {
			t.Fatalf("%s: Decode(nil) = %v, want nil", c.Name(), got)
		}
	}
}

func TestCodecSilence(t *testing.T) {
	for _, tc := range []struct {
		codec Codec
		want  byte
	}{
		{CodecMulaw, MulawSilence},
		{CodecAlaw, AlawSilence},
	} {
		if got := tc.codec.Silence(0); got != nil {
			t.Fatalf("%s: Silence(0) = %v, want nil", tc.codec.Name(), got)
		}
		if got := tc.codec.Silence(-3); got != nil {
			t.Fatalf("%s: Silence(-3) = %v, want nil", tc.codec.Name(), got)
		}
		got := tc.codec.Silence(160)
		if len(got) != 160 {
			t.Fatalf("%s: Silence(160) len = %d, want 160", tc.codec.Name(), len(got))
		}
		for i, b := range got {
			if b != tc.want {
				t.Fatalf("%s: Silence byte[%d] = %#x, want %#x", tc.codec.Name(), i, b, tc.want)
			}
		}
		// Decoding encoded silence must yield ~zero PCM (idle line).
		pcm := tc.codec.Decode(got)
		for i := 0; i+1 < len(pcm); i += 2 {
			if v := int16(binary.LittleEndian.Uint16(pcm[i:])); v < -8 || v > 8 {
				t.Fatalf("%s: decoded silence sample %d = %d, expected ~0", tc.codec.Name(), i/2, v)
			}
		}
	}
}

func TestCodecForPayloadType(t *testing.T) {
	cases := []struct {
		pt   uint8
		ok   bool
		name string
	}{
		{0, true, "PCMU"},
		{8, true, "PCMA"},
		{9, false, ""},   // G722 — unsupported in the media plane
		{96, false, ""},  // Opus
		{101, false, ""}, // telephone-event
	}
	for _, tc := range cases {
		c, ok := CodecForPayloadType(tc.pt)
		if ok != tc.ok {
			t.Fatalf("CodecForPayloadType(%d) ok = %v, want %v", tc.pt, ok, tc.ok)
		}
		if ok && c.Name() != tc.name {
			t.Fatalf("CodecForPayloadType(%d) = %q, want %q", tc.pt, c.Name(), tc.name)
		}
		if !ok && c != nil {
			t.Fatalf("CodecForPayloadType(%d) returned non-nil codec on failure", tc.pt)
		}
	}
}

func TestCodecForName(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
		want string
	}{
		{"PCMU", true, "PCMU"},
		{"pcmu", true, "PCMU"},
		{" Mu-Law ", true, "PCMU"},
		{"ulaw", true, "PCMU"},
		{"G711U", true, "PCMU"},
		{"PCMA", true, "PCMA"},
		{"a-law", true, "PCMA"},
		{"ALAW", true, "PCMA"},
		{"G711A", true, "PCMA"},
		{"G722", false, ""},
		{"opus", false, ""},
		{"", false, ""},
	}
	for _, tc := range cases {
		c, ok := CodecForName(tc.name)
		if ok != tc.ok {
			t.Fatalf("CodecForName(%q) ok = %v, want %v", tc.name, ok, tc.ok)
		}
		if ok && c.Name() != tc.want {
			t.Fatalf("CodecForName(%q) = %q, want %q", tc.name, c.Name(), tc.want)
		}
		if !ok && c != nil {
			t.Fatalf("CodecForName(%q) returned non-nil codec on failure", tc.name)
		}
	}
}

func TestG711CodecForName(t *testing.T) {
	cases := []struct {
		name   string
		wantPT uint8
	}{
		{"PCMA", PayloadTypePCMA},
		{"pcma", PayloadTypePCMA},
		{"PCMU", PayloadTypePCMU},
		{"", PayloadTypePCMU},     // empty -> default µ-law
		{"G722", PayloadTypePCMU}, // removed/unknown -> default µ-law
		{"opus", PayloadTypePCMU},
	}
	for _, tc := range cases {
		if got := G711CodecForName(tc.name); got == nil || got.PayloadType() != tc.wantPT {
			t.Fatalf("G711CodecForName(%q) PT = %v, want %d", tc.name, got, tc.wantPT)
		}
	}
}

func bytesEqual(a, b []byte) bool {
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
