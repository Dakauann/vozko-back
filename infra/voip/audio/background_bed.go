package audio

import "encoding/binary"

// backgroundBed is a looped, pre-attenuated PCM16 ambience mixed softly under
// every outbound RTP frame, so a call carries a constant low background (e.g. a
// soft keyboard sound) for its entire duration.
//
// The sample buffer is immutable and shared read-only across every concurrent
// call; only the loop cursor is per-stream. Mixing happens in the linear PCM
// domain (decode → add → clamp → re-encode) because G.711 companded octets
// cannot be summed directly. The mix runs only for streams that opted in, so a
// call without a bed pays exactly zero overhead.
type backgroundBed struct {
	samples []int16 // pre-scaled mono PCM16 at the stream's sample rate
	pos     int     // loop cursor, advanced under the stream lock
}

// newBackgroundBed wraps a shared, read-only sample buffer with a per-stream
// cursor. It returns nil when there is nothing to play so the hot path can skip
// mixing with a single nil check.
func newBackgroundBed(samples []int16) *backgroundBed {
	if len(samples) == 0 {
		return nil
	}
	return &backgroundBed{samples: samples}
}

// mixFrame folds the next len(payload) bed samples into an already-encoded
// G.711 frame IN PLACE. It is called once per frame while the stream lock is
// held, so the loop cursor advances without a data race and wraps seamlessly to
// keep the bed continuous across frame boundaries.
func (b *backgroundBed) mixFrame(payload []byte, codec Codec) {
	if b == nil || len(b.samples) == 0 || len(payload) == 0 {
		return
	}
	pcm := codec.Decode(payload) // 2 little-endian bytes per companded octet
	for i := 0; i+1 < len(pcm); i += 2 {
		speech := int32(int16(binary.LittleEndian.Uint16(pcm[i:])))
		mixed := speech + int32(b.samples[b.pos])
		if mixed > 32767 {
			mixed = 32767
		} else if mixed < -32768 {
			mixed = -32768
		}
		binary.LittleEndian.PutUint16(pcm[i:], uint16(int16(mixed)))
		b.pos++
		if b.pos >= len(b.samples) {
			b.pos = 0
		}
	}
	copy(payload, codec.Encode(pcm))
}
