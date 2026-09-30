package voip

import (
	"encoding/binary"
	"math"
	"time"
)

const (
	toneAmplitude = 0.2 * math.MaxInt16
	toneRamp      = 5 * time.Millisecond
)

type Tone struct {
	Frequency float64
	Cadence   []time.Duration
}

var (
	BrazilRingback   = Tone{Frequency: 425, Cadence: []time.Duration{time.Second, 4 * time.Second}}
	BrazilBusy       = Tone{Frequency: 425, Cadence: []time.Duration{250 * time.Millisecond, 250 * time.Millisecond}}
	BrazilCongestion = Tone{Frequency: 425, Cadence: []time.Duration{250 * time.Millisecond, 250 * time.Millisecond, 750 * time.Millisecond, 250 * time.Millisecond}}
)

type ToneSource struct {
	tone     Tone
	segments []int
	cycle    int
	ramp     int
	sample   int
}

func NewToneSource(tone Tone) *ToneSource {
	segments := make([]int, len(tone.Cadence))
	cycle := 0
	for i, span := range tone.Cadence {
		segments[i] = samplesIn(span)
		cycle += segments[i]
	}
	return &ToneSource{tone: tone, segments: segments, cycle: cycle, ramp: samplesIn(toneRamp)}
}

func (s *ToneSource) Next() []byte {
	frame := make([]byte, PCMFrameBytes)
	for i := 0; i < PCMFrameBytes/2; i++ {
		binary.LittleEndian.PutUint16(frame[i*2:], uint16(int16(s.level())))
		s.sample++
	}
	return frame
}

func (s *ToneSource) level() float64 {
	if s.cycle == 0 {
		return 0
	}
	offset := s.sample % s.cycle
	for i, length := range s.segments {
		if offset >= length {
			offset -= length
			continue
		}
		if i%2 == 1 {
			return 0
		}
		phase := 2 * math.Pi * s.tone.Frequency * float64(s.sample) / PCMSampleRate
		return toneAmplitude * s.envelope(offset, length) * math.Sin(phase)
	}
	return 0
}

func (s *ToneSource) envelope(offset, length int) float64 {
	edge := min(offset, length-1-offset)
	if edge >= s.ramp {
		return 1
	}
	return 0.5 - 0.5*math.Cos(math.Pi*float64(edge)/float64(s.ramp))
}

func samplesIn(span time.Duration) int {
	return int(span * PCMSampleRate / time.Second)
}
