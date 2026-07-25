package ws

import (
	"sync"

	calls_usecase "vozko/usecases/calls"
)

// sipDefaultSampleRate is the narrowband rate every SIP trunk leg runs at; the
// browser uplink is resampled down to it before hitting the RTP path.
const sipDefaultSampleRate = 8000

var allowedInboundSampleRates = map[int]struct{}{
	8000:  {},
	16000: {},
	22050: {},
	24000: {},
	32000: {},
	44100: {},
	48000: {},
}

type inboundAudioConverter struct {
	mu      sync.Mutex
	srcRate int
	rs      *calls_usecase.Resampler
}

func (c *inboundAudioConverter) Convert(pcm []byte, sampleRate int) ([]byte, bool) {
	if sampleRate == 0 {
		sampleRate = sipDefaultSampleRate
	}
	if _, ok := allowedInboundSampleRates[sampleRate]; !ok {
		return nil, false
	}
	if sampleRate == sipDefaultSampleRate {
		return pcm, true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rs == nil || c.srcRate != sampleRate {
		c.rs = calls_usecase.NewResampler(sampleRate, sipDefaultSampleRate)
		c.srcRate = sampleRate
	}
	out := c.rs.Process(pcm)
	return out, true
}
