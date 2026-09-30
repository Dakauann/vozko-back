package calls_usecase

import (
	"sync"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/voip"
)

const progressToneEventBuffer = 16

type ProgressTonePlan struct {
	Ringback   voip.Tone
	Busy       voip.Tone
	Congestion voip.Tone
	Closing    time.Duration
}

var BrazilProgressTones = ProgressTonePlan{
	Ringback:   voip.BrazilRingback,
	Busy:       voip.BrazilBusy,
	Congestion: voip.BrazilCongestion,
	Closing:    3 * time.Second,
}

func (p ProgressTonePlan) closingTone(outcome conversation.CallEventType) (voip.Tone, bool) {
	switch outcome {
	case conversation.CallEventBusy, conversation.CallEventDeclined:
		return p.Busy, true
	case conversation.CallEventFailed:
		return p.Congestion, true
	}
	return voip.Tone{}, false
}

type ProgressToneCall struct {
	inner  conversation.CRMCall
	plan   ProgressTonePlan
	audio  chan []byte
	events chan conversation.CallEvent
	done   chan struct{}

	hangupOnce sync.Once
	hungUp     chan struct{}
}

func NewProgressToneCall(inner conversation.CRMCall, plan ProgressTonePlan) *ProgressToneCall {
	call := &ProgressToneCall{
		inner:  inner,
		plan:   plan,
		audio:  make(chan []byte, recordingCRMCallForwardBuffer),
		events: make(chan conversation.CallEvent, progressToneEventBuffer),
		done:   make(chan struct{}),
		hungUp: make(chan struct{}),
	}
	go call.run()
	return call
}

func (c *ProgressToneCall) ID() string                            { return c.inner.ID() }
func (c *ProgressToneCall) SendAudio(pcm16 []byte) error          { return c.inner.SendAudio(pcm16) }
func (c *ProgressToneCall) AudioStream() <-chan []byte            { return c.audio }
func (c *ProgressToneCall) Events() <-chan conversation.CallEvent { return c.events }
func (c *ProgressToneCall) Done() <-chan struct{}                 { return c.done }
func (c *ProgressToneCall) Contact() (conversation.CallContact, bool) {
	return conversation.ContactOf(c.inner)
}

func (c *ProgressToneCall) Hangup() error {
	c.hangupOnce.Do(func() { close(c.hungUp) })
	return c.inner.Hangup()
}

func (c *ProgressToneCall) run() {
	defer close(c.done)
	defer close(c.events)
	defer close(c.audio)

	innerAudio, innerEvents, innerDone := c.inner.AudioStream(), c.inner.Events(), c.inner.Done()
	ringback := newTonePlayer()
	defer ringback.stop()
	farEndHeard, answered := false, false
	for {
		select {
		case frame, ok := <-innerAudio:
			if !ok {
				innerAudio = nil
				continue
			}
			if !farEndHeard && voip.Audible(frame) {
				farEndHeard = true
				ringback.stop()
			}
			if !ringback.playing() {
				c.push(frame)
			}
		case <-ringback.tick():
			c.push(ringback.next())
		case <-c.hungUp:
			ringback.stop()
		case <-innerDone:
			ringback.stop()
			if outcome, ok := conversation.PendingOutcome(innerEvents); ok {
				c.close(outcome)
			}
			return
		case event, ok := <-innerEvents:
			if !ok {
				return
			}
			switch {
			case event.Type == conversation.CallEventAlerting:
				if !answered && !farEndHeard {
					ringback.start(c.plan.Ringback)
				}
				continue
			case event.Type == conversation.CallEventAnswered:
				answered = true
				ringback.stop()
			case event.IsTerminal():
				ringback.stop()
				c.close(event)
				return
			}
			c.emit(event)
		}
	}
}

func (c *ProgressToneCall) close(outcome conversation.CallEvent) {
	if tone, ok := c.plan.closingTone(outcome.Type); ok {
		c.play(tone, c.plan.Closing)
	}
	c.emit(outcome)
}

func (c *ProgressToneCall) play(tone voip.Tone, span time.Duration) {
	player := newTonePlayer()
	player.start(tone)
	defer player.stop()
	deadline := time.NewTimer(span)
	defer deadline.Stop()
	for {
		select {
		case <-c.hungUp:
			return
		case <-deadline.C:
			return
		case <-player.tick():
			c.push(player.next())
		}
	}
}

func (c *ProgressToneCall) emit(event conversation.CallEvent) {
	select {
	case c.events <- event:
		return
	default:
	}
	select {
	case c.events <- event:
	case <-c.hungUp:
	}
}

func (c *ProgressToneCall) push(frame []byte) {
	for {
		select {
		case c.audio <- frame:
			return
		default:
		}
		select {
		case <-c.audio:
		default:
		}
	}
}

type tonePlayer struct {
	source *voip.ToneSource
	ticker *time.Ticker
}

func newTonePlayer() *tonePlayer { return &tonePlayer{} }

func (p *tonePlayer) start(tone voip.Tone) {
	if p.ticker != nil {
		return
	}
	p.source = voip.NewToneSource(tone)
	p.ticker = time.NewTicker(voip.PCMFrameDuration)
}

func (p *tonePlayer) stop() {
	if p.ticker == nil {
		return
	}
	p.ticker.Stop()
	p.ticker, p.source = nil, nil
}

func (p *tonePlayer) playing() bool { return p.ticker != nil }

func (p *tonePlayer) tick() <-chan time.Time {
	if p.ticker == nil {
		return nil
	}
	return p.ticker.C
}

func (p *tonePlayer) next() []byte { return p.source.Next() }
