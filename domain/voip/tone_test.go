package voip

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

func samplesOf(frame []byte) []float64 {
	out := make([]float64, len(frame)/2)
	for i := range out {
		out[i] = float64(int16(binary.LittleEndian.Uint16(frame[i*2:])))
	}
	return out
}

func rms(samples []float64) float64 {
	sum := 0.0
	for _, s := range samples {
		sum += s * s
	}
	return math.Sqrt(sum / float64(len(samples)))
}

func dominantFrequency(samples []float64) float64 {
	crossings := 0
	for i := 1; i < len(samples); i++ {
		if (samples[i-1] < 0) != (samples[i] < 0) {
			crossings++
		}
	}
	return float64(crossings) / 2 / (float64(len(samples)) / PCMSampleRate)
}

func framesOf(tone Tone, span time.Duration) [][]float64 {
	source := NewToneSource(tone)
	frames := make([][]float64, int(span/PCMFrameDuration))
	for i := range frames {
		frames[i] = samplesOf(source.Next())
	}
	return frames
}

func TestBrazilianRingbackIsFourHundredTwentyFiveHertzOneSecondOnFourOff(t *testing.T) {
	frames := framesOf(BrazilRingback, 10*time.Second)
	for i, frame := range frames {
		inSecond := time.Duration(i%250) * PCMFrameDuration
		sounding := rms(frame) > 1000
		if inSecond >= 20*time.Millisecond && inSecond < 980*time.Millisecond && !sounding {
			t.Fatalf("frame %d at %v is silent, want tone", i, inSecond)
		}
		if inSecond >= 1020*time.Millisecond && inSecond < 4980*time.Millisecond && rms(frame) > 1 {
			t.Fatalf("frame %d at %v is sounding, want silence", i, inSecond)
		}
	}
	var tone []float64
	for _, frame := range frames[5:45] {
		tone = append(tone, frame...)
	}
	if got := dominantFrequency(tone); math.Abs(got-425) > 5 {
		t.Fatalf("ringback frequency = %.1f Hz, want 425", got)
	}
}

func TestBrazilianBusyAlternatesAQuarterSecondOnAndOff(t *testing.T) {
	frames := framesOf(BrazilBusy, 2*time.Second)
	for i, frame := range frames {
		inCycle := time.Duration(i%25) * PCMFrameDuration
		if inCycle >= 20*time.Millisecond && inCycle < 220*time.Millisecond && rms(frame) < 1000 {
			t.Fatalf("frame %d is silent during the tone", i)
		}
		if inCycle >= 270*time.Millisecond && inCycle < 480*time.Millisecond && rms(frame) > 1 {
			t.Fatalf("frame %d sounds during the pause", i)
		}
	}
}

func TestBrazilianCongestionFollowsItsShortLongCadence(t *testing.T) {
	frames := framesOf(BrazilCongestion, 3*time.Second)
	period := 75
	on := func(from, to time.Duration) {
		for i := int(from / PCMFrameDuration); i < int(to/PCMFrameDuration); i++ {
			for cycle := 0; cycle+i < len(frames); cycle += period {
				if rms(frames[cycle+i]) < 1000 {
					t.Fatalf("frame %d silent, want tone", cycle+i)
				}
			}
		}
	}
	off := func(from, to time.Duration) {
		for i := int(from / PCMFrameDuration); i < int(to/PCMFrameDuration); i++ {
			for cycle := 0; cycle+i < len(frames); cycle += period {
				if rms(frames[cycle+i]) > 1 {
					t.Fatalf("frame %d sounds, want silence", cycle+i)
				}
			}
		}
	}
	on(20*time.Millisecond, 220*time.Millisecond)
	off(270*time.Millisecond, 480*time.Millisecond)
	on(520*time.Millisecond, 1220*time.Millisecond)
	off(1270*time.Millisecond, 1480*time.Millisecond)
}

func TestToneFramesAreTelephonySized(t *testing.T) {
	source := NewToneSource(BrazilRingback)
	for range 300 {
		if frame := source.Next(); len(frame) != PCMFrameBytes {
			t.Fatalf("frame is %d bytes, want %d", len(frame), PCMFrameBytes)
		}
	}
}

func TestToneEdgesRampInsteadOfClicking(t *testing.T) {
	first := samplesOf(NewToneSource(BrazilBusy).Next())
	if math.Abs(first[0]) > 200 || math.Abs(first[1]) > 400 {
		t.Fatalf("tone starts at %.0f, %.0f: the onset must ramp from silence", first[0], first[1])
	}
}

func TestAudibilityTellsSpeechFromLineSilence(t *testing.T) {
	if Audible(make([]byte, PCMFrameBytes)) {
		t.Fatal("digital silence counted as audible")
	}
	hiss := make([]byte, PCMFrameBytes)
	for i := 0; i < len(hiss); i += 2 {
		hiss[i] = byte(i % 40)
	}
	if Audible(hiss) {
		t.Fatal("line noise counted as audible")
	}
	if !Audible(framesBytes(BrazilBusy, 2)) {
		t.Fatal("a tone was not audible")
	}
}

func framesBytes(tone Tone, n int) []byte {
	source := NewToneSource(tone)
	var out []byte
	for range n {
		out = source.Next()
	}
	return out
}
