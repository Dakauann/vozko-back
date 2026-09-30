package voip

import (
	"encoding/binary"
	"math"
)

const audibleRMS = 300

func Audible(pcm []byte) bool {
	samples := len(pcm) / 2
	if samples == 0 {
		return false
	}
	sum := 0.0
	for i := 0; i < samples; i++ {
		sample := float64(int16(binary.LittleEndian.Uint16(pcm[i*2:])))
		sum += sample * sample
	}
	return math.Sqrt(sum/float64(samples)) >= audibleRMS
}
