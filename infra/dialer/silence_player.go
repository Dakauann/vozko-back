package dialer

import (
	"context"
	"sync"
	"time"
)

type SilenceSink interface {
	SendAudio(pcm16 []byte) error
}

const (
	sipBytesPerFrame = 320
	sipFrameInterval = 20 * time.Millisecond
)

// HoldPlayer pumps hold audio (music on hold, a comfort tone, ringback, or
// silence when src is nil) into a call leg's SendAudio on the telephony 20ms
// cadence. It is the SOLE writer to the leg while hold is active, the WS
// handler's uplink gate and the lifecycle's holdActive downlink gate guarantee
// that, so the caller hears exactly this source and nothing else.
//
// Start/Stop are once-guarded and idempotent; Stop blocks until the pump
// goroutine exits so a swap can safely attach the leg right after.
type HoldPlayer struct {
	sink     SilenceSink
	src      HoldAudioSource
	silence  []byte
	interval time.Duration

	startOnce sync.Once
	stopOnce  sync.Once
	cancel    context.CancelFunc
	done      chan struct{}
}

// SilencePlayer is the original name of the hold pump (it only ever played
// zero frames). Kept as an alias so existing call sites and the atomic pointer
// on liveDialerCall stay source-compatible; new code should say HoldPlayer.
type SilencePlayer = HoldPlayer

// NewHoldPlayer builds a pump feeding src into sink. A nil src plays silence.
func NewHoldPlayer(sink SilenceSink, src HoldAudioSource) *HoldPlayer {
	return &HoldPlayer{
		sink:     sink,
		src:      src,
		silence:  make([]byte, sipBytesPerFrame),
		interval: sipFrameInterval,
		done:     make(chan struct{}),
	}
}

// NewSilencePlayer builds a pump that plays pure silence (the legacy behavior).
func NewSilencePlayer(sink SilenceSink) *HoldPlayer {
	return NewHoldPlayer(sink, nil)
}

func (p *HoldPlayer) Start(parent context.Context) {
	if p == nil {
		return
	}
	p.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(parent)
		p.cancel = cancel
		go func() {
			defer close(p.done)
			t := time.NewTicker(p.interval)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					frame := p.silence
					if p.src != nil {
						if f := p.src.NextFrame(); len(f) == sipBytesPerFrame {
							frame = f
						}
					}
					_ = p.sink.SendAudio(frame)
				}
			}
		}()
	})
}

func (p *HoldPlayer) Stop() {
	if p == nil {
		return
	}
	p.stopOnce.Do(func() {
		if p.cancel == nil {
			return
		}
		p.cancel()
		<-p.done
	})
}
