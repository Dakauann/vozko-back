package webhook_usecase

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"vozko/domain/cache"
	"vozko/domain/messaging"
)

// ConsumerRunner is the shared webhook-consumer skeleton.
//
// The same ~40 lines — unmarshal, dedup, semaphore, goroutine, panic recover,
// ack — are copy-pasted across four consumer packages today, each differing only
// in the payload type and the log prefix. This generic runner exists so channels
// added from Instagram onward describe only what is actually channel-specific:
// the dedup key, the handler, and how a failure should be classified.
//
// It layers two dedup stages:
//
//   - a Redis fast path, which fails CLOSED (a Redis error requeues rather than
//     risking a double-process), and
//   - an optional durable claim in Postgres, which still rejects a replay after
//     the Redis key has expired or been evicted.
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

// DurableDedup is the persistent idempotency store. Optional: when nil, only the
// Redis fast path applies.
type DurableDedup interface {
	Claim(ctx context.Context, key, channel, accountID string) (bool, error)
}

// Disposition tells the runner what to do with a failed message.
type Disposition int

const (
	// DispositionRetry re-publishes with backoff; the failure looks transient.
	DispositionRetry Disposition = iota
	// DispositionDrop acks without retrying: retrying can never succeed (an
	// unknown account, a permanently invalid payload).
	DispositionDrop
	// DispositionDeadLetter parks the message for an operator.
	DispositionDeadLetter
)

// ConsumerConfig configures a runner.
type ConsumerConfig[T any] struct {
	// Name is the log prefix and the channel recorded in the durable dedup store.
	Name  string
	Topic string

	QueueSub messaging.MessageQueueSub
	QueuePub messaging.MessageQueuePub

	SharedState cache.SharedState
	DedupTTL    time.Duration
	Durable     DurableDedup

	// Concurrency bounds in-flight handlers for this topic.
	Concurrency int
	// Timeout bounds one handler invocation.
	Timeout time.Duration

	// DedupKey derives the idempotency key. An empty key skips dedup.
	DedupKey func(*T) string
	// Handle processes one message.
	Handle func(context.Context, *T) error
	// Classify decides what to do with a failure. Defaults to retry.
	Classify func(error) Disposition
}

// NewConsumerRunner builds a runner.
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

// Start subscribes to the topic.
func (r *ConsumerRunner[T]) Start() error {
	return r.queueSub.Subscribe(r.topic, func(payload []byte, ack messaging.MessageAck) {
		r.dispatch(payload, ack)
	})
}

func (r *ConsumerRunner[T]) dispatch(raw []byte, ack messaging.MessageAck) {
	var payload T
	if err := json.Unmarshal(raw, &payload); err != nil {
		// A payload we cannot parse will never parse. Nack without requeue
		// instead of looping forever.
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
			// Fail closed: without a working guard we cannot tell a duplicate
			// from a first delivery, and double-processing a message is worse
			// than a redelivery.
			log.Printf("[%s] dedup unavailable, requeueing: %v", r.name, err)
			_ = ack.Nack(true)
			return
		}
		switch result {
		case IdempotencyAcquireDuplicate:
			_ = ack.Ack()
			return
		case IdempotencyAcquireInProgress:
			// Another consumer holds this key. Re-publish with delay so
			// per-conversation ordering is preserved by reordering into the
			// future rather than by blocking the delivery.
			r.requeueWithDelay(raw, ack)
			return
		}
	}

	r.semaphore <- struct{}{}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[%s] panic in handler: %v", r.name, rec)
				if key != "" {
					_ = r.dedup.Release(key)
				}
				_ = ack.Nack(true)
			}
		}()
		defer func() { <-r.semaphore }()

		ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
		defer cancel()

		// The durable claim runs inside the handler goroutine so a Postgres
		// round trip does not stall the delivery loop.
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
			// Release the key so the retry is not mistaken for a duplicate.
			if key != "" {
				_ = r.dedup.Release(key)
			}
			if attempt >= messaging.MaxRetries {
				log.Printf("[%s] retries exhausted after %d attempts, dead-lettering", r.name, attempt)
				r.deadLetter(raw, ack)
				return
			}
			// Deliberately NOT Nack(requeue=true).
			//
			// A plain requeue puts the message straight back on the head of the same
			// queue with no delay AND no x-death header. DeliveryCount() is derived
			// from x-death, which RabbitMQ only stamps when a message is actually
			// dead-lettered — so a requeued message reports attempt 1 forever, the
			// exhaustion check above can never fire, and a permanently failing
			// message spins the consumer at full CPU indefinitely. A single bad
			// payload did exactly that: hundreds of identical attempts inside one
			// second, none of them ever reaching the DLQ.
			//
			// Routing through the delay queue is what makes the attempt counter
			// advance, so backoff and exhaustion both work.
			r.requeueWithDelay(raw, ack)
		}
	}()
}

// requeueWithDelay re-publishes a message that lost an in-flight race.
func (r *ConsumerRunner[T]) requeueWithDelay(raw []byte, ack messaging.MessageAck) {
	delay := r.retryBase * time.Duration(1<<minInt(ack.DeliveryCount(), 4))
	if delay > r.retryMax {
		delay = r.retryMax
	}
	if r.queuePub != nil {
		if err := r.queuePub.PublishWithDelay(r.topic, raw, delay); err != nil {
			// Nack here is a bounded risk rather than a spin: the delay publish
			// failing is an infrastructure fault, not a poison payload.
			log.Printf("[%s] delayed requeue failed, nacking instead: %v", r.name, err)
			_ = ack.Nack(true)
			return
		}
		_ = ack.Ack()
		return
	}
	// Without a publisher there is no way to delay, and an immediate requeue would
	// spin. Park the message so it can be replayed by hand.
	log.Printf("[%s] no queue publisher configured; cannot delay retry, dead-lettering", r.name)
	r.deadLetter(raw, ack)
}

// deadLetter parks a message on the topic's DLQ.
//
// The WhatsApp pipeline has no DLQ at all: exhausted retries are logged and
// acked, so the event is gone. Parking it keeps an operator story — the message
// can be inspected and replayed.
func (r *ConsumerRunner[T]) deadLetter(raw []byte, ack messaging.MessageAck) {
	if r.queuePub == nil {
		_ = ack.Ack()
		return
	}
	if err := r.queuePub.Publish(r.topic+messaging.DLQSuffix, raw); err != nil {
		// Requeue rather than lose it, even though that risks a loop; an
		// operator alert on DLQ publish failure is the intended follow-up.
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
