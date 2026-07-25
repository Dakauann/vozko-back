package calls_usecase

import (
	"encoding/binary"
	"math"
)

type Resampler struct {
	srcRate int
	dstRate int

	filterCoeffs []float64
	filterState  []float64

	upsampleCoeffs []float64
	upsampleState  []float64

	srcPosFrac float64
	prevTail   [4]float64
	hasPrev    bool

	samplesBuf   []float64
	filterBuf    []float64
	filterOutBuf []float64
	interpBuf    []float64
	encodeBuf    []byte
}

func NewResampler(srcRate, dstRate int) *Resampler {
	r := &Resampler{
		srcRate: srcRate,
		dstRate: dstRate,
	}
	if dstRate < srcRate {

		r.filterCoeffs = designLPFilter(srcRate, dstRate)
		r.filterState = make([]float64, len(r.filterCoeffs)-1)
	} else if dstRate > srcRate {

		fc := float64(srcRate) / float64(2*dstRate)
		r.upsampleCoeffs = designLPFilterWithCutoff(fc)
		r.upsampleState = make([]float64, len(r.upsampleCoeffs)-1)
	}
	return r
}

func (r *Resampler) Process(data []byte) []byte {
	if r.srcRate <= 0 || r.dstRate <= 0 || len(data) < 2 {
		return append([]byte(nil), data...)
	}
	if r.srcRate == r.dstRate {
		return append([]byte(nil), data...)
	}

	srcSamples := len(data) / 2
	if srcSamples == 0 {
		return nil
	}

	if cap(r.samplesBuf) < srcSamples {
		r.samplesBuf = make([]float64, srcSamples)
	}
	samples := r.samplesBuf[:srcSamples]
	for i := 0; i < srcSamples; i++ {
		samples[i] = float64(int16(binary.LittleEndian.Uint16(data[i*2:])))
	}

	if r.filterCoeffs != nil {
		samples = r.applyFilter(samples)
	}

	output := r.interpolateF64(samples)
	if len(output) == 0 {
		return nil
	}

	if r.upsampleCoeffs != nil {
		output = r.applyOutputFilter(output)
	}

	return r.encodeSamplesF64(output)
}

func (r *Resampler) applyFilter(input []float64) []float64 {
	M := len(r.filterCoeffs)
	overlap := M - 1

	bufLen := overlap + len(input)
	if cap(r.filterBuf) < bufLen {
		r.filterBuf = make([]float64, bufLen)
	}
	buf := r.filterBuf[:bufLen]
	copy(buf, r.filterState)
	copy(buf[overlap:], input)

	outLen := len(input)
	if cap(r.filterOutBuf) < outLen {
		r.filterOutBuf = make([]float64, outLen)
	}
	output := r.filterOutBuf[:outLen]
	for i := 0; i < outLen; i++ {
		var sum float64
		pos := overlap + i
		for j := 0; j < M; j++ {
			sum += r.filterCoeffs[j] * buf[pos-j]
		}
		output[i] = sum
	}

	copy(r.filterState, buf[bufLen-overlap:])

	return output
}

func (r *Resampler) interpolateF64(samples []float64) []float64 {
	srcSamples := len(samples)
	ratio := float64(r.srcRate) / float64(r.dstRate)

	srcEnd := float64(srcSamples)
	var dstSamples int
	pos := r.srcPosFrac
	for pos < srcEnd {
		dstSamples++
		pos += ratio
	}

	if dstSamples == 0 {
		r.srcPosFrac = pos - float64(srcSamples)
		r.updateTail(samples)
		return nil
	}

	out := r.interpBuf[:0]
	if cap(r.interpBuf) < dstSamples {
		r.interpBuf = make([]float64, dstSamples)
	}
	out = r.interpBuf[:dstSamples]

	getSample := func(idx int) float64 {
		if idx >= 0 && idx < srcSamples {
			return samples[idx]
		}
		if idx < 0 && r.hasPrev {
			tailIdx := len(r.prevTail) + idx
			if tailIdx >= 0 && tailIdx < len(r.prevTail) {
				return r.prevTail[tailIdx]
			}
			if srcSamples > 0 {
				return samples[0]
			}
			return 0
		}
		if srcSamples > 0 {
			return samples[srcSamples-1]
		}
		return 0
	}

	srcPos := r.srcPosFrac
	for i := 0; i < dstSamples; i++ {
		idx := int(math.Floor(srcPos))
		frac := srcPos - float64(idx)

		s0 := getSample(idx)
		s1 := getSample(idx + 1)
		out[i] = s0 + frac*(s1-s0)
		srcPos += ratio
	}

	r.srcPosFrac = srcPos - float64(srcSamples)
	r.updateTail(samples)

	return out
}

func (r *Resampler) updateTail(samples []float64) {
	n := len(samples)
	r.hasPrev = true
	if n >= 4 {
		r.prevTail = [4]float64{
			samples[n-4], samples[n-3],
			samples[n-2], samples[n-1],
		}
	} else {

		copy(r.prevTail[:], r.prevTail[n:])
		for i := 0; i < n; i++ {
			r.prevTail[4-n+i] = samples[i]
		}
	}
}

func designLPFilter(srcRate, dstRate int) []float64 {
	fc := float64(dstRate) / float64(2*srcRate)
	return designLPFilterWithCutoff(fc)
}

func designLPFilterWithCutoff(fc float64) []float64 {
	const numTaps = 47
	coeffs := make([]float64, numTaps)
	mid := float64(numTaps-1) / 2.0

	var sum float64
	for i := 0; i < numTaps; i++ {
		n := float64(i) - mid

		var sinc float64
		if math.Abs(n) < 1e-10 {
			sinc = 2 * fc
		} else {
			sinc = math.Sin(2*math.Pi*fc*n) / (math.Pi * n)
		}

		window := 0.42 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(numTaps-1)) +
			0.08*math.Cos(4*math.Pi*float64(i)/float64(numTaps-1))

		coeffs[i] = sinc * window
		sum += coeffs[i]
	}

	for i := range coeffs {
		coeffs[i] /= sum
	}

	return coeffs
}

func (r *Resampler) applyOutputFilter(input []float64) []float64 {
	M := len(r.upsampleCoeffs)
	overlap := M - 1

	bufLen := overlap + len(input)
	if cap(r.filterBuf) < bufLen {
		r.filterBuf = make([]float64, bufLen)
	}
	buf := r.filterBuf[:bufLen]
	copy(buf, r.upsampleState)
	copy(buf[overlap:], input)

	outLen := len(input)
	if cap(r.filterOutBuf) < outLen {
		r.filterOutBuf = make([]float64, outLen)
	}
	output := r.filterOutBuf[:outLen]
	for i := 0; i < outLen; i++ {
		var sum float64
		pos := overlap + i
		for j := 0; j < M; j++ {
			sum += r.upsampleCoeffs[j] * buf[pos-j]
		}
		output[i] = sum
	}

	copy(r.upsampleState, buf[bufLen-overlap:])
	return output
}

func (r *Resampler) encodeSamplesF64(samples []float64) []byte {
	needed := len(samples) * 2
	if cap(r.encodeBuf) < needed {
		r.encodeBuf = make([]byte, needed)
	}
	out := r.encodeBuf[:needed]
	for i, s := range samples {
		if s > 32767 {
			s = 32767
		} else if s < -32768 {
			s = -32768
		}
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16(math.Round(s))))
	}
	return out
}
