package copilotscreen

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	"vozko/domain/copilot"
)

const (
	RepliesChannel    = "copilot:screen_replies"
	repliesPerCommand = 4
)

type PubSub interface {
	Publish(channel string, data []byte) error
	Subscribe(ctx context.Context, channel string, handler func(data []byte))
}

type envelope struct {
	Key   string              `json:"key"`
	Reply copilot.ScreenReply `json:"reply"`
}

type Broker struct {
	pubsub PubSub

	mu      sync.Mutex
	waiters map[string]chan copilot.ScreenReply
}

func NewBroker(pubsub PubSub) *Broker {
	return &Broker{pubsub: pubsub, waiters: map[string]chan copilot.ScreenReply{}}
}

func (b *Broker) Start(ctx context.Context) {
	b.pubsub.Subscribe(ctx, RepliesChannel, b.deliverLocal)
}

func (b *Broker) Expect(key string) (func(ctx context.Context) (copilot.ScreenReply, error), func()) {
	ch := make(chan copilot.ScreenReply, repliesPerCommand)
	b.mu.Lock()
	b.waiters[key] = ch
	b.mu.Unlock()
	wait := func(ctx context.Context) (copilot.ScreenReply, error) {
		select {
		case reply := <-ch:
			return reply, nil
		case <-ctx.Done():
			return copilot.ScreenReply{}, ctx.Err()
		}
	}
	cancel := func() {
		b.mu.Lock()
		if b.waiters[key] == ch {
			delete(b.waiters, key)
		}
		b.mu.Unlock()
	}
	return wait, cancel
}

func (b *Broker) Deliver(key string, reply copilot.ScreenReply) error {
	data, err := json.Marshal(envelope{Key: key, Reply: reply})
	if err != nil {
		return err
	}
	return b.pubsub.Publish(RepliesChannel, data)
}

func (b *Broker) deliverLocal(data []byte) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil || env.Key == "" {
		log.Printf("[copilot] dropped malformed screen reply: %v", err)
		return
	}
	b.mu.Lock()
	ch, ok := b.waiters[env.Key]
	b.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- env.Reply:
	default:
	}
}

func (b *Broker) waiting() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.waiters)
}

var _ copilot.ScreenMailbox = (*Broker)(nil)
