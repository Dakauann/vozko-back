package webhook_usecase

import (
	"context"
	"encoding/json"
	"log"
	"runtime/debug"
	"time"

	"vozko/domain/cache"
	"vozko/domain/messaging"
)

type ConsumerRunner[T any] struct {
	name      string
	topic     string
	queueSub  messaging.MessageQueueSub
	queuePub  messaging.MessageQueuePub
	dedup     *IdempotencyGuard
	durable   DurableDedup
	semaphore chan struct{}

	dedupKey  func(*T) string
	handle    func(context.Context, *T) error
	classify  func(error) Disposition
	timeout   time.Duration
	retryBase time.Duration
	retryMax  time.Duration
}

type DurableDedup interface {
	Claim(ctx context.Context, key, channel, accountID string) (bool, error)
}

type Disposition int

const (
	DispositionRetry Disposition = iota
	DispositionDrop
	DispositionDeadLetter
)

type ConsumerConfig[T any] struct {
	Name  string
	Topic string

	QueueSub messaging.MessageQueueSub
	QueuePub messaging.MessageQueuePub

	SharedState cache.SharedState
	DedupTTL    time.Duration
	Durable     DurableDedup

	Concurrency int
	Timeout     time.Duration

	DedupKey func(*T) string
	Handle   func(context.Context, *T) error
	Classify func(error) Disposition
}

func NewConsumerRunner[T any](cfg ConsumerConfig[T]) *ConsumerRunner[T] {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 10
	}
	if cfg.DedupTTL <= 0 {
		cfg.DedupTTL = 5 * time.Minute
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Minute
	}
	if cfg.Classify == nil {
		cfg.Classify = func(error) Disposition { return DispositionRetry }
	}
	return &ConsumerRunner[T]{
		name:      cfg.Name,
		topic:     cfg.Topic,
		queueSub:  cfg.QueueSub,
		queuePub:  cfg.QueuePub,
		dedup:     NewIdempotencyGuard(cfg.SharedState, cfg.DedupTTL),
		durable:   cfg.Durable,
		semaphore: make(chan struct{}, cfg.Concurrency),
		dedupKey:  cfg.DedupKey,
		handle:    cfg.Handle,
		classify:  cfg.Classify,
		timeout:   cfg.Timeout,
		retryBase: 2 * time.Second,
		retryMax:  30 * time.Second,
	}
}

func (r *ConsumerRunner[T]) Start() error {
	return r.queueSub.Subscribe(r.topic, func(payload []byte, ack messaging.MessageAck) {
		r.dispatch(payload, ack)
	})
}

func (r *ConsumerRunner[T]) dispatch(raw []byte, ack messaging.MessageAck) {
	var payload T
	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("[%s] invalid payload, discarding: %v", r.name, err)
		_ = ack.Nack(false)
		return
	}

	key := ""
	if r.dedupKey != nil {
		key = r.dedupKey(&payload)
	}

	if key != "" {
		result, err := r.dedup.Acquire(key)
		if err != nil {
			log.Printf("[%s] dedup unavailable, requeueing: %v", r.name, err)
			_ = ack.Nack(true)
			return
		}
		switch result {
		case IdempotencyAcquireDuplicate:
			_ = ack.Ack()
			return
		case IdempotencyAcquireInProgress:
			r.requeueWithDelay(raw, ack)
			return
		}
	}

	r.semaphore <- struct{}{}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[%s] panic in handler: %v\n%s", r.name, rec, debug.Stack())
				if key != "" {
					_ = r.dedup.Release(key)
				}
				r.deadLetter(raw, ack)
			}
		}()
		defer func() { <-r.semaphore }()

		ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
		defer cancel()

		if key != "" && r.durable != nil {
			claimed, err := r.durable.Claim(ctx, key, r.name, "")
			if err != nil {
				log.Printf("[%s] durable dedup failed, falling back to redis guard: %v", r.name, err)
			} else if !claimed {
				_ = r.dedup.Complete(key)
				_ = ack.Ack()
				return
			}
		}

		err := r.handle(ctx, &payload)
		if err == nil {
			if key != "" {
				_ = r.dedup.Complete(key)
			}
			if ackErr := ack.Ack(); ackErr != nil {
				log.Printf("[%s] ack failed: %v", r.name, ackErr)
			}
			return
		}

		switch r.classify(err) {
		case DispositionDrop:
			log.Printf("[%s] dropping message (not retryable): %v", r.name, err)
			if key != "" {
				_ = r.dedup.Complete(key)
			}
			_ = ack.Ack()

		case DispositionDeadLetter:
			log.Printf("[%s] dead-lettering message: %v", r.name, err)
			if key != "" {
				_ = r.dedup.Release(key)
			}
			r.deadLetter(raw, ack)

		default:
			attempt := ack.DeliveryCount()
			log.Printf("[%s] retryable failure (attempt %d/%d): %v", r.name, attempt, messaging.MaxRetries, err)
			if key != "" {
				_ = r.dedup.Release(key)
			}
			if attempt >= messaging.MaxRetries {
				log.Printf("[%s] retries exhausted after %d attempts, dead-lettering", r.name, attempt)
				r.deadLetter(raw, ack)
				return
			}
			r.requeueWithDelay(raw, ack)
		}
	}()
}

func (r *ConsumerRunner[T]) requeueWithDelay(raw []byte, ack messaging.MessageAck) {
	delay := r.retryBase * time.Duration(1<<minInt(ack.DeliveryCount(), 4))
	if delay > r.retryMax {
		delay = r.retryMax
	}
	if r.queuePub != nil {
		if err := r.queuePub.PublishWithDelay(r.topic, raw, delay); err != nil {
			log.Printf("[%s] delayed requeue failed, nacking instead: %v", r.name, err)
			_ = ack.Nack(true)
			return
		}
		_ = ack.Ack()
		return
	}
	log.Printf("[%s] no queue publisher configured; cannot delay retry, dead-lettering", r.name)
	r.deadLetter(raw, ack)
}

func (r *ConsumerRunner[T]) deadLetter(raw []byte, ack messaging.MessageAck) {
	if r.queuePub == nil {
		_ = ack.Ack()
		return
	}
	if err := r.queuePub.Publish(r.topic+messaging.DLQSuffix, raw); err != nil {
		log.Printf("[%s] DLQ publish failed, requeueing: %v", r.name, err)
		_ = ack.Nack(true)
		return
	}
	_ = ack.Ack()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
