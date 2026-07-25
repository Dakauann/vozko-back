package audio

import (
	"strings"

	"github.com/zaf/g711"
)

// G.711 RTP payload types (RFC 3551, Table 4). These are the only voice codecs
// the platform exchanges on the wire today; see docs/SIP_AUDIO_PIPELINE.md §0/§2.
const (
	PayloadTypePCMU uint8 = 0 // G.711 µ-law
	PayloadTypePCMA uint8 = 8 // G.711 A-law
)

// Codec converts between 16-bit little-endian linear PCM and a G.711 RTP
// payload. It bundles everything the send, record and packet-loss-concealment
// paths need (encode, decode, idle/silence octet, payload type) so that no
// caller has to branch on the companding law. All methods are pure and
// allocation-light, which keeps them trivially unit-testable.
//
// Today the only implementations are µ-law (PCMU) and A-law (PCMA). The
// interface is the seam through which a future wideband codec (e.g. Opus on
// all-IP legs — see docs/SIP_AUDIO_PIPELINE.md §8.2/§10 P5) would be added
// without touching the RTP plumbing.
type Codec interface {
	// Name is the SDP encoding name ("PCMU" / "PCMA").
	Name() string
	// PayloadType is the static RTP payload type (0 for PCMU, 8 for PCMA).
	PayloadType() uint8
	// Encode converts little-endian PCM16 to the on-wire payload.
	Encode(pcm16 []byte) []byte
	// Decode converts an on-wire payload back to little-endian PCM16.
	Decode(payload []byte) []byte
	// SilenceByte is the single idle octet for this law (µ-law 0xFF, A-law 0xD5).
	SilenceByte() byte
	// Silence returns n octets of encoded silence, for PLC / underrun fill.
	Silence(n int) []byte
}

type g711Codec struct {
	name        string
	payloadType uint8
	silence     byte
	encode      func([]byte) []byte
	decode      func([]byte) []byte
}

func (c g711Codec) Name() string       { return c.name }
func (c g711Codec) PayloadType() uint8 { return c.payloadType }
func (c g711Codec) Encode(pcm16 []byte) []byte {
	if len(pcm16) == 0 {
		return nil
	}
	return c.encode(pcm16)
}
func (c g711Codec) Decode(payload []byte) []byte {
	if len(payload) == 0 {
		return nil
	}
	return c.decode(payload)
}
func (c g711Codec) SilenceByte() byte { return c.silence }
func (c g711Codec) Silence(n int) []byte {
	if n <= 0 {
		return nil
	}
	out := make([]byte, n)
	for i := range out {
		out[i] = c.silence
	}
	return out
}

// CodecMulaw / CodecAlaw are the two shared, stateless G.711 codecs. They are
// safe for concurrent use (no mutable state). MulawSilence (0xFF) and
// AlawSilence (0xD5) are defined alongside the RTP stream / reorder buffer.
var (
	CodecMulaw Codec = g711Codec{
		name:        "PCMU",
		payloadType: PayloadTypePCMU,
		silence:     MulawSilence,
		encode:      g711.EncodeUlaw,
		decode:      g711.DecodeUlaw,
	}
	CodecAlaw Codec = g711Codec{
		name:        "PCMA",
		payloadType: PayloadTypePCMA,
		silence:     AlawSilence,
		encode:      g711.EncodeAlaw,
		decode:      g711.DecodeAlaw,
	}
)

// CodecForPayloadType resolves a codec from an RTP payload type. ok is false
// for anything other than PCMU (0) / PCMA (8) — callers should treat that as
// "unsupported payload" and skip it (the existing decode sites already do).
func CodecForPayloadType(pt uint8) (Codec, bool) {
	switch pt {
	case PayloadTypePCMU:
		return CodecMulaw, true
	case PayloadTypePCMA:
		return CodecAlaw, true
	default:
		return nil, false
	}
}

// G711CodecForName maps a negotiated codec name (e.g. from
// voip.MediaSession.NegotiatedCodec().Name) to the concrete G.711 codec used to
// encode/decode RTP. It is the single mapping every channel uses, and it
// defaults to µ-law (PCMU) for an empty/unknown name so callers never need their
// own fallback. See docs/SIP_AUDIO_PIPELINE.md §10 (P1).
func G711CodecForName(name string) Codec {
	if c, ok := CodecForName(name); ok {
		return c
	}
	return CodecMulaw
}

// CodecForName resolves a codec from an SDP/negotiated codec name. It accepts
// the common aliases so it can be fed directly from MediaInfo.SelectedCodec.Name
// or a config value. ok is false for unsupported / wideband names.
func CodecForName(name string) (Codec, bool) {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "PCMU", "G711U", "ULAW", "MULAW", "MU-LAW":
		return CodecMulaw, true
	case "PCMA", "G711A", "ALAW", "A-LAW":
		return CodecAlaw, true
	default:
		return nil, false
	}
}
