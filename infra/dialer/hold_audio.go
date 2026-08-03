package dialer

import (
	"encoding/binary"
	"math"
	"sync"
)

// HoldAudioSource yields the audio a held/parked caller hears, one 20ms
// telephony frame (320 bytes, 16-bit LE mono PCM at 8kHz) per call. Sources are
// stateful cursors and are pulled from a single goroutine (the HoldPlayer's
// ticker), so they need no internal locking. A nil source means silence.
type HoldAudioSource interface {
	NextFrame() []byte
}

// HoldAudioFactory builds a fresh source per held leg (each leg needs its own
// loop cursor), resolved for the leg's workspace. The container wires the
// resolution chain: the workspace's configured hold music, else the global
// HOLD_MUSIC_PATH asset, else the generated comfort tone.
type HoldAudioFactory func(workspaceID string) HoldAudioSource

// --- looping PCM (music on hold from a decoded asset) ---------------------

type loopingPCMSource struct {
	pcm   []byte // immutable, shared read-only across all sources
	pos   int
	frame []byte
}

// NewLoopingPCMSource loops a decoded 16-bit LE mono 8kHz PCM buffer (the exact
// output of audio.LoadMP3AsTelephonyPCM) forever. Returns nil for buffers
// shorter than one frame so callers fall back to silence instead of a stutter.
func NewLoopingPCMSource(pcm16 []byte) HoldAudioSource {
	if len(pcm16) < sipBytesPerFrame {
		return nil
	}
	// Keep the loop frame-aligned so every NextFrame is a single wrap-free copy
	// boundary check, never a partial sample.
	usable := len(pcm16) - (len(pcm16) % 2)
	return &loopingPCMSource{pcm: pcm16[:usable], frame: make([]byte, sipBytesPerFrame)}
}

func (s *loopingPCMSource) NextFrame() []byte {
	for filled := 0; filled < sipBytesPerFrame; {
		n := copy(s.frame[filled:], s.pcm[s.pos:])
		filled += n
		s.pos += n
		if s.pos >= len(s.pcm) {
			s.pos = 0
		}
	}
	return s.frame
}

// --- generated comfort hold tone (default when no music asset is shipped) --

// The default hold audio when no HOLD_MUSIC_PATH asset is configured: a soft
// two-note chime every few seconds over silence. It is the standard contact
// center comfort tone pattern, it tells the caller "you are still connected,
// please hold" without licensing a music track, and it replaces the dead air
// that made held callers hang up. Rendered once, shared read-only.
var (
	holdToneOnce sync.Once
	holdToneLoop []byte
)

func holdComfortLoop() []byte {
	holdToneOnce.Do(func() {
		holdToneLoop = renderChimeLoop(4*telephonySampleRate, []chime{
			{startSample: 0, freqA: 440, freqB: 660, durSamples: 2400, amp: 0.16},
		})
	})
	return holdToneLoop
}

// NewComfortToneSource returns the generated default hold audio.
func NewComfortToneSource() HoldAudioSource {
	return NewLoopingPCMSource(holdComfortLoop())
}

// --- generated ringback (Brazilian cadence) --------------------------------

// Ringback: 425Hz, 1s on / 4s off (the Brazilian PSTN cadence). Used as an
// auditory cue while the RECALL wave rings the original agent, mirroring
// Asterisk's TRANSFER_RETRANSFER "transferee hears ringing during this state".
var (
	ringbackOnce sync.Once
	ringbackLoop []byte
)

func ringbackCadenceLoop() []byte {
	ringbackOnce.Do(func() {
		ringbackLoop = renderChimeLoop(5*telephonySampleRate, []chime{
			{startSample: 0, freqA: 425, freqB: 0, durSamples: telephonySampleRate, amp: 0.22, sustain: true},
		})
	})
	return ringbackLoop
}

// NewRingbackToneSource returns the generated ringback cadence.
func NewRingbackToneSource() HoldAudioSource {
	return NewLoopingPCMSource(ringbackCadenceLoop())
}

const telephonySampleRate = 8000

// chime is one tone burst inside a rendered loop: an optional dual sine with an
// exponential decay envelope (or a flat sustain for ringback-style tones).
type chime struct {
	startSample int
	freqA       float64
	freqB       float64 // 0 = single tone
	durSamples  int
	amp         float64
	sustain     bool // true = flat envelope (ringback); false = struck-note decay
}

func renderChimeLoop(totalSamples int, chimes []chime) []byte {
	out := make([]byte, totalSamples*2)
	for _, c := range chimes {
		for i := 0; i < c.durSamples && c.startSample+i < totalSamples; i++ {
			t := float64(i) / telephonySampleRate
			env := 1.0
			if !c.sustain {
				env = math.Exp(-6 * float64(i) / float64(c.durSamples))
			}
			v := math.Sin(2 * math.Pi * c.freqA * t)
			if c.freqB > 0 {
				v = (v + math.Sin(2*math.Pi*c.freqB*t)) / 2
			}
			sample := int16(c.amp * env * v * 32767)
			binary.LittleEndian.PutUint16(out[(c.startSample+i)*2:], uint16(sample))
		}
	}
	return out
}
