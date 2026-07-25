package audio

import (
	"encoding/binary"
	"math"
	"testing"
)

func constPCM(n int, v int16) []byte {
	out := make([]byte, n*2)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(v))
	}
	return out
}

func sampleAt(pcm []byte, i int) int16 {
	return int16(binary.LittleEndian.Uint16(pcm[i*2:]))
}

func TestSampleCount(t *testing.T) {
	if SampleCount(nil) != 0 || SampleCount([]byte{1}) != 0 || SampleCount([]byte{1, 2, 3, 4}) != 2 {
		t.Fatal("SampleCount wrong")
	}
}

func TestRMSAmplitude(t *testing.T) {
	if got := RMSAmplitude(nil); got != 0 {
		t.Fatalf("RMS(nil) = %v, want 0", got)
	}
	if got := RMSAmplitude([]byte{0x00}); got != 0 {
		t.Fatalf("RMS(sub-sample) = %v, want 0", got)
	}
	// Constant 1000 -> RMS 1000.
	if got := RMSAmplitude(constPCM(50, 1000)); math.Abs(got-1000) > 1e-6 {
		t.Fatalf("RMS = %v, want 1000", got)
	}
}

func TestPeakAmplitude(t *testing.T) {
	if PeakAmplitude(nil) != 0 {
		t.Fatal("peak(nil) != 0")
	}
	pcm := pcm16(10, -2000, 1500, 32767)
	if got := PeakAmplitude(pcm); got != 32767 {
		t.Fatalf("peak = %d, want 32767", got)
	}
	// -32768 must clamp to 32767, not overflow to a negative magnitude.
	if got := PeakAmplitude(pcm16(-32768)); got != 32767 {
		t.Fatalf("peak(-32768) = %d, want 32767", got)
	}
}

func TestApplyGainLimited(t *testing.T) {
	// Empty input.
	if got := ApplyGainLimited(nil, 2); len(got) != 0 {
		t.Fatalf("gain(nil) len = %d, want 0", len(got))
	}
	// 2x gain on a mid-level signal.
	out := ApplyGainLimited(pcm16(1000, -1000), 2)
	if sampleAt(out, 0) != 2000 || sampleAt(out, 1) != -2000 {
		t.Fatalf("2x gain wrong: %d %d", sampleAt(out, 0), sampleAt(out, 1))
	}
	// Saturation on both rails.
	sat := ApplyGainLimited(pcm16(20000, -20000), 4)
	if sampleAt(sat, 0) != 32767 || sampleAt(sat, 1) != -32768 {
		t.Fatalf("saturation wrong: %d %d", sampleAt(sat, 0), sampleAt(sat, 1))
	}
	// Non-positive gain is treated as unity (never silences/inverts).
	unity := ApplyGainLimited(pcm16(123, -456), 0)
	if sampleAt(unity, 0) != 123 || sampleAt(unity, 1) != -456 {
		t.Fatalf("zero gain should be unity, got %d %d", sampleAt(unity, 0), sampleAt(unity, 1))
	}
	neg := ApplyGainLimited(pcm16(123), -5)
	if sampleAt(neg, 0) != 123 {
		t.Fatalf("negative gain should be unity, got %d", sampleAt(neg, 0))
	}
	// Input must not be mutated.
	in := pcm16(1000)
	_ = ApplyGainLimited(in, 4)
	if sampleAt(in, 0) != 1000 {
		t.Fatal("ApplyGainLimited mutated its input")
	}
}

func TestNormalizeRMSBoostsQuiet(t *testing.T) {
	// Quiet 500-RMS signal, target 4500 -> gain 9 but clamped to maxGain 8.
	in := constPCM(100, 500)
	out := NormalizeRMS(in, 4500, 8)
	if got := RMSAmplitude(out); math.Abs(got-4000) > 1 { // 500 * 8
		t.Fatalf("normalized RMS = %v, want ~4000 (gain clamped to 8x)", got)
	}
}

func TestNormalizeRMSReachesTarget(t *testing.T) {
	// 1000-RMS signal, target 3000 -> gain 3 (< maxGain), reaches target.
	in := constPCM(100, 1000)
	out := NormalizeRMS(in, 3000, 8)
	if got := RMSAmplitude(out); math.Abs(got-3000) > 1 {
		t.Fatalf("normalized RMS = %v, want ~3000", got)
	}
}

func TestNormalizeRMSNeverAttenuates(t *testing.T) {
	// Loud 8000-RMS signal, target 4500 -> gain < 1 -> unchanged.
	in := constPCM(100, 8000)
	out := NormalizeRMS(in, 4500, 8)
	if !bytesEqual(in, out) {
		t.Fatal("loud signal must be returned unchanged (boost-only)")
	}
}

func TestNormalizeRMSEdgeCases(t *testing.T) {
	// Empty.
	if got := NormalizeRMS(nil, 4500, 8); len(got) != 0 {
		t.Fatalf("normalize(nil) len = %d", len(got))
	}
	// Silence -> unchanged copy.
	sil := make([]byte, 40)
	out := NormalizeRMS(sil, 4500, 8)
	if !bytesEqual(sil, out) {
		t.Fatal("silence must be unchanged")
	}
	// Defaults applied when params non-positive (quiet signal still boosts).
	in := constPCM(100, 500)
	def := NormalizeRMS(in, 0, 0)
	if RMSAmplitude(def) <= RMSAmplitude(in) {
		t.Fatal("default params should still boost a quiet signal")
	}
	// Returned buffer must be a copy, not the input alias, even when unchanged.
	loud := constPCM(10, 9000)
	if got := NormalizeRMS(loud, 4500, 8); &got[0] == &loud[0] {
		t.Fatal("NormalizeRMS must return a copy, not the input slice")
	}
}

func TestSaturateInt16Rounding(t *testing.T) {
	cases := []struct {
		in   float64
		want int16
	}{
		{0, 0},
		{2.4, 2},
		{2.5, 3},
		{-2.5, -3},
		{40000, 32767},
		{-40000, -32768},
		{32766.6, 32767},
	}
	for _, tc := range cases {
		if got := saturateInt16(tc.in); got != tc.want {
			t.Fatalf("saturateInt16(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
