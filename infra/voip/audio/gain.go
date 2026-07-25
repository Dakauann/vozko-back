package audio

import (
	"encoding/binary"
	"math"
)

// Linear PCM16 (little-endian, mono) level helpers for output loudness control.
// See docs/SIP_AUDIO_PIPELINE.md §10 (P3). The goal is to hand the carrier's
// transcoder a consistent level: quiet TTS/agent audio is gently boosted toward
// a target RMS, while a hard saturating limiter guarantees no inter-sample
// overflow/clipping. Everything here is pure and deterministic — apply it to a
// whole TTS segment so a single buffer carries one stable gain (no pumping).

const (
	// pcm16FullScale is the maximum magnitude of a signed 16-bit sample.
	pcm16FullScale = 32767.0

	// DefaultOutputTargetRMS / DefaultOutputMaxGain match the established
	// preprocessing convention used on the STT path (~4500 RMS, 8x ceiling),
	// chosen for narrowband speech.
	DefaultOutputTargetRMS = 4500.0
	DefaultOutputMaxGain   = 8.0
)

// SampleCount returns the number of whole PCM16 samples in buf.
func SampleCount(pcm16 []byte) int { return len(pcm16) / 2 }

// RMSAmplitude returns the root-mean-square sample magnitude of buf (0 for an
// empty or sub-sample buffer).
func RMSAmplitude(pcm16 []byte) float64 {
	n := SampleCount(pcm16)
	if n == 0 {
		return 0
	}
	var sumSq float64
	for i := 0; i < n; i++ {
		s := float64(int16(binary.LittleEndian.Uint16(pcm16[i*2:])))
		sumSq += s * s
	}
	return math.Sqrt(sumSq / float64(n))
}

// PeakAmplitude returns the largest absolute sample magnitude in buf, clamped to
// the int16 range (so a lone -32768 reports as 32767).
func PeakAmplitude(pcm16 []byte) int {
	n := SampleCount(pcm16)
	peak := 0
	for i := 0; i < n; i++ {
		s := int(int16(binary.LittleEndian.Uint16(pcm16[i*2:])))
		if s < 0 {
			s = -s
		}
		if s > pcm16FullScale {
			s = pcm16FullScale
		}
		if s > peak {
			peak = s
		}
	}
	return peak
}

// ApplyGainLimited multiplies every sample by gain and saturates the result to
// the int16 range. gain <= 0 is treated as unity (the buffer is copied
// unchanged) so the function never silences or inverts audio by accident. The
// input is not mutated; a fresh buffer is returned.
func ApplyGainLimited(pcm16 []byte, gain float64) []byte {
	n := SampleCount(pcm16)
	out := make([]byte, n*2)
	if n == 0 {
		return out
	}
	if gain <= 0 {
		gain = 1
	}
	for i := 0; i < n; i++ {
		v := float64(int16(binary.LittleEndian.Uint16(pcm16[i*2:]))) * gain
		binary.LittleEndian.PutUint16(out[i*2:], uint16(saturateInt16(v)))
	}
	return out
}

// NormalizeRMS scales buf toward targetRMS and hard-limits to avoid clipping.
// It is boost-only: the gain is clamped to [1.0, maxGain], so loud audio is
// never attenuated and a silent or already-loud buffer is returned unchanged
// (a copy). Peaks are saturated to int16 by ApplyGainLimited. Defaults are used
// when targetRMS or maxGain are non-positive.
func NormalizeRMS(pcm16 []byte, targetRMS, maxGain float64) []byte {
	if SampleCount(pcm16) == 0 {
		return append([]byte(nil), pcm16...)
	}
	if targetRMS <= 0 {
		targetRMS = DefaultOutputTargetRMS
	}
	if maxGain <= 0 {
		maxGain = DefaultOutputMaxGain
	}

	rms := RMSAmplitude(pcm16)
	if rms <= 0 {
		// Pure silence — nothing to boost.
		return append([]byte(nil), pcm16...)
	}

	gain := targetRMS / rms
	if gain <= 1 {
		// Already at/above target: don't attenuate.
		return append([]byte(nil), pcm16...)
	}
	if gain > maxGain {
		gain = maxGain
	}
	return ApplyGainLimited(pcm16, gain)
}

// saturateInt16 rounds and clamps a float sample to the int16 range.
func saturateInt16(v float64) int16 {
	if v >= pcm16FullScale {
		return 32767
	}
	if v <= -32768.0 {
		return -32768
	}
	if v >= 0 {
		return int16(v + 0.5)
	}
	return int16(v - 0.5)
}
