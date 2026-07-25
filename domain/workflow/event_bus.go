package workflow

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type EventKind string

const (
	EventSpeechStart EventKind = "speech_start"

	EventDTMFDigit EventKind = "dtmf_digit"

	EventHangup EventKind = "hangup"

	EventFirstFrameSent EventKind = "first_frame_sent"
)

type RuntimeEvent struct {
	Kind    EventKind
	At      time.Time
	Payload any
}

type EventBus interface {
	Publish(ev RuntimeEvent)

	Subscribe(filter ...EventKind) (<-chan RuntimeEvent, func())

	InterruptContext(parent context.Context, kinds ...EventKind) (context.Context, func())
}

func NewInMemoryEventBus() EventBus {
	return &inMemoryEventBus{
		subs: make(map[uint64]*eventSubscription),
	}
}

type inMemoryEventBus struct {
	mu             sync.RWMutex
	subs           map[uint64]*eventSubscription
	nextID         uint64
	firstFrameSent atomic.Bool
}

type eventSubscription struct {
	id    uint64
	kinds map[EventKind]struct{}
	ch    chan RuntimeEvent

	mu     sync.Mutex
	closed bool
}

func (s *eventSubscription) wants(kind EventKind) bool {
	if s.kinds == nil {
		return true
	}
	_, ok := s.kinds[kind]
	return ok
}

func (s *eventSubscription) deliver(ev RuntimeEvent) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	select {
	case s.ch <- ev:
		return true
	default:
		return false
	}
}

func (s *eventSubscription) closeOnce() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

const eventSubscriberBuffer = 8

func (b *inMemoryEventBus) Publish(ev RuntimeEvent) {
	if ev.Kind == EventFirstFrameSent {

		b.firstFrameSent.Store(true)
	}
	if ev.Kind == EventSpeechStart && !b.firstFrameSent.Load() {

		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}

	b.mu.RLock()
	targets := make([]*eventSubscription, 0, len(b.subs))
	for _, s := range b.subs {
		if s.wants(ev.Kind) {
			targets = append(targets, s)
		}
	}
	b.mu.RUnlock()

	for _, s := range targets {

		_ = s.deliver(ev)
	}
}

func (b *inMemoryEventBus) Subscribe(filter ...EventKind) (<-chan RuntimeEvent, func()) {
	sub := &eventSubscription{
		ch: make(chan RuntimeEvent, eventSubscriberBuffer),
	}
	if len(filter) > 0 {
		sub.kinds = make(map[EventKind]struct{}, len(filter))
		for _, k := range filter {
			sub.kinds[k] = struct{}{}
		}
	}

	b.mu.Lock()
	b.nextID++
	sub.id = b.nextID
	b.subs[sub.id] = sub
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		delete(b.subs, sub.id)
		b.mu.Unlock()
		sub.closeOnce()
	}
	return sub.ch, cancel
}

func (b *inMemoryEventBus) InterruptContext(parent context.Context, kinds ...EventKind) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	if len(kinds) == 0 {

		return ctx, cancel
	}

	ch, unsub := b.Subscribe(kinds...)
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ch:
			if !ok {
				return
			}
			cancel()
		}
	}()

	release := func() {
		cancel()
		unsub()
		<-done
	}
	return ctx, release
}
