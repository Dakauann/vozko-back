package webchat

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	wcdomain "vozko/domain/webchat"
)

const (
	EventsChannel        = "webchat:events"
	MaxStreamsPerVisitor = 3
	MaxStreamsPerReplica = 2000
	streamBuffer         = 32
)

type PubSub interface {
	Publish(channel string, data []byte) error
	Subscribe(ctx context.Context, channel string, handler func(data []byte))
}

type envelope struct {
	VisitorID string                `json:"visitorId"`
	Event     wcdomain.VisitorEvent `json:"event"`
}

type Broker struct {
	pubsub PubSub

	mu      sync.Mutex
	streams map[string]map[chan wcdomain.VisitorEvent]struct{}
	total   int
}

func NewBroker(pubsub PubSub) *Broker {
	return &Broker{pubsub: pubsub, streams: map[string]map[chan wcdomain.VisitorEvent]struct{}{}}
}

func (b *Broker) Start(ctx context.Context) {
	b.pubsub.Subscribe(ctx, EventsChannel, b.deliver)
}

func (b *Broker) Publish(_ context.Context, event wcdomain.VisitorEvent) error {
	if event.VisitorID == "" {
		return wcdomain.ErrVisitorNotFound
	}
	data, err := json.Marshal(envelope{VisitorID: event.VisitorID, Event: event})
	if err != nil {
		return err
	}
	return b.pubsub.Publish(EventsChannel, data)
}

func (b *Broker) Subscribe(visitorID string) (<-chan wcdomain.VisitorEvent, func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.total >= MaxStreamsPerReplica || len(b.streams[visitorID]) >= MaxStreamsPerVisitor {
		return nil, nil, wcdomain.ErrRateLimited
	}
	ch := make(chan wcdomain.VisitorEvent, streamBuffer)
	if b.streams[visitorID] == nil {
		b.streams[visitorID] = map[chan wcdomain.VisitorEvent]struct{}{}
	}
	b.streams[visitorID][ch] = struct{}{}
	b.total++

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			delete(b.streams[visitorID], ch)
			if len(b.streams[visitorID]) == 0 {
				delete(b.streams, visitorID)
			}
			b.total--
			close(ch)
		})
	}
	return ch, cancel, nil
}

func (b *Broker) deliver(data []byte) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil || env.VisitorID == "" {
		log.Printf("[webchat] dropped malformed event: %v", err)
		return
	}
	env.Event.VisitorID = env.VisitorID

	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.streams[env.VisitorID] {
		select {
		case ch <- env.Event:
		default:
			log.Printf("[webchat] stream for visitor=%s is full, event %s dropped (resynced on reconnect)", env.VisitorID, env.Event.Kind)
		}
	}
}

var (
	_ wcdomain.EventPublisher = (*Broker)(nil)
	_ wcdomain.EventStream    = (*Broker)(nil)
)
