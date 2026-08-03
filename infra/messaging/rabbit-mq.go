package queue

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"vozko/domain/messaging"

	amqp "github.com/rabbitmq/amqp091-go"
)

func getRabbitMQHost() string {
	host := os.Getenv("RABBITMQ_HOST")
	if host == "" {
		return "localhost"
	}
	return host
}

func getRabbitMQPort() string {
	port := os.Getenv("RABBITMQ_PORT")
	if port == "" {
		return "5672"
	}
	return port
}

type RabbitMQQueuePub struct {
	exchange string
	pool     *ConnectionPool

	mu      sync.Mutex
	channel *amqp.Channel
	release func()

	declaredTopics map[string]struct{}
}

func NewRabbitMQQueuePub(
	pool *ConnectionPool, exchange string,
) messaging.MessageQueuePub {
	return &RabbitMQQueuePub{
		pool:           pool,
		exchange:       exchange,
		declaredTopics: make(map[string]struct{}),
	}
}

func (q *RabbitMQQueuePub) ensureChannelLocked() (retCh *amqp.Channel, retErr error) {
	if q.channel != nil {
		return q.channel, nil
	}

	ch, release, err := q.pool.OpenChannel()
	if err != nil {
		return nil, fmt.Errorf("failed to acquire channel from pool: %w", err)
	}

	ok := false
	defer func() {
		if !ok {

			release()
			func() { defer func() { recover() }(); _ = ch.Close() }()
		}
	}()

	if err := ch.Confirm(false); err != nil {
		return nil, fmt.Errorf("failed to enable publisher confirms: %w", err)
	}

	if err = ch.ExchangeDeclare(
		q.exchange,
		amqp.ExchangeDirect,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return nil, fmt.Errorf("failed to declare exchange: %w", err)
	}

	ok = true
	q.channel = ch
	q.release = release
	return ch, nil
}

func (q *RabbitMQQueuePub) invalidateChannelLocked() {
	if q.channel != nil {
		_ = q.channel.Close()
		q.channel = nil
	}
	if q.release != nil {
		q.release()
		q.release = nil
	}

	if len(q.declaredTopics) > 0 {
		q.declaredTopics = make(map[string]struct{})
	}
}

func (q *RabbitMQQueuePub) publishLocked(routingKey string, msg amqp.Publishing) error {
	channel, err := q.ensureChannelLocked()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	confirmation, err := channel.PublishWithDeferredConfirmWithContext(
		ctx,
		q.exchange,
		routingKey,
		false,
		false,
		msg,
	)
	if err != nil {
		q.invalidateChannelLocked()
		return fmt.Errorf("failed to publish message: %w", err)
	}

	confirmed, err := confirmation.WaitContext(ctx)
	if err != nil {
		q.invalidateChannelLocked()
		return fmt.Errorf("publish confirm timeout for %s: %w", routingKey, err)
	}
	if !confirmed {
		q.invalidateChannelLocked()
		return fmt.Errorf("message to %s was nacked by broker", routingKey)
	}

	return nil
}

func (q *RabbitMQQueuePub) ensureDelayTopologyLocked(topic string) error {
	if q.declaredTopics == nil {
		q.declaredTopics = make(map[string]struct{})
	}
	if _, ok := q.declaredTopics[topic]; ok {

		if q.channel != nil {
			return nil
		}
	}

	channel, err := q.ensureChannelLocked()
	if err != nil {
		return err
	}

	if _, err := channel.QueueDeclare(topic, true, false, false, false, nil); err != nil {
		q.invalidateChannelLocked()
		return fmt.Errorf("failed to declare queue %s: %w", topic, err)
	}
	if err := channel.QueueBind(topic, topic, q.exchange, false, nil); err != nil {
		q.invalidateChannelLocked()
		return fmt.Errorf("failed to bind queue %s: %w", topic, err)
	}

	delayTopic := topic + messaging.DelayQueueSuffix
	delayArgs := amqp.Table{
		"x-dead-letter-exchange":    q.exchange,
		"x-dead-letter-routing-key": topic,
	}
	channel, err = declareDelayQueue(channel, func() (*amqp.Channel, error) {
		q.invalidateChannelLocked()
		return q.ensureChannelLocked()
	}, delayTopic, delayArgs)
	if err != nil {
		q.invalidateChannelLocked()
		return fmt.Errorf("failed to declare delay queue %s: %w", delayTopic, err)
	}
	if err := channel.QueueBind(delayTopic, delayTopic, q.exchange, false, nil); err != nil {
		q.invalidateChannelLocked()
		return fmt.Errorf("failed to bind delay queue %s: %w", delayTopic, err)
	}

	q.declaredTopics[topic] = struct{}{}
	return nil
}

// isPreconditionFailed reports whether err is RabbitMQ's 406 PRECONDITION_FAILED,
// which is what a redeclare with different arguments than the existing durable
// queue returns (and it closes the channel).
func isPreconditionFailed(err error) bool {
	var amqpErr *amqp.Error
	if errors.As(err, &amqpErr) {
		return amqpErr.Code == amqp.PreconditionFailed
	}
	return false
}

// declareDelayQueue declares the per-topic delay queue that holds messages until
// their TTL expires and dead-letters them onto the real topic.
//
// The queue is durable and its x-dead-letter-exchange is baked in at creation, so
// a queue created against a PREVIOUS exchange name can never be redeclared, the
// broker answers 406 and the consumer refuses to start, silently stranding every
// delayed message. When that happens, drop the stale queue and recreate it with
// the current arguments. Its contents are only un-elapsed timers, and they are
// already unreachable: the alternative is a consumer that never boots at all.
func declareDelayQueue(ch *amqp.Channel, reconnect func() (*amqp.Channel, error), delayTopic string, args amqp.Table) (*amqp.Channel, error) {
	if _, err := ch.QueueDeclare(delayTopic, true, false, false, false, args); err == nil {
		return ch, nil
	} else if !isPreconditionFailed(err) {
		return ch, err
	}

	// The failed declare closed the channel; get a fresh one to repair with.
	fresh, err := reconnect()
	if err != nil {
		return ch, fmt.Errorf("reopen channel to rebuild %s: %w", delayTopic, err)
	}

	purged, err := fresh.QueueDelete(delayTopic, false, false, false)
	if err != nil {
		return fresh, fmt.Errorf("delete stale delay queue %s: %w", delayTopic, err)
	}
	log.Printf("rabbitmq: delay queue %s had stale arguments (dead-letter exchange changed); recreated, %d pending message(s) dropped", delayTopic, purged)

	if _, err := fresh.QueueDeclare(delayTopic, true, false, false, false, args); err != nil {
		return fresh, err
	}
	return fresh, nil
}

func (q *RabbitMQQueuePub) Publish(topic string, message []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.publishLocked(topic, amqp.Publishing{
		ContentType:  "application/json",
		Body:         message,
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
	})
}

func (q *RabbitMQQueuePub) PublishWithDelay(topic string, message []byte, delay time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if err := q.ensureDelayTopologyLocked(topic); err != nil {
		return err
	}

	delayTopic := topic + messaging.DelayQueueSuffix
	ms := int(delay.Milliseconds())
	if ms < 1 {
		ms = 1
	}
	return q.publishLocked(delayTopic, amqp.Publishing{
		ContentType:  "application/json",
		Body:         message,
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
		Expiration:   fmt.Sprintf("%d", ms),
	})
}

func (q *RabbitMQQueuePub) ValidateConnection() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, err := q.ensureChannelLocked()
	return err
}

func (q *RabbitMQQueuePub) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.invalidateChannelLocked()
	return nil
}

type RabbitMQQueueSub struct {
	pool     *ConnectionPool
	exchange string

	mu       sync.Mutex
	channels map[string]*amqp.Channel
	releases map[string]func()
	stopped  map[string]bool
}

func NewRabbitMQQueueSub(
	pool *ConnectionPool, exchange string,
) messaging.MessageQueueSub {
	return &RabbitMQQueueSub{
		pool:     pool,
		exchange: exchange,
		channels: make(map[string]*amqp.Channel),
		releases: make(map[string]func()),
		stopped:  make(map[string]bool),
	}
}

// prefetchByTopic overrides the default per-channel prefetch (QoS) for specific
// high-throughput topics. webhook.whatsapp.message is processed by a concurrent
// worker pool (ordering is guarded by a per-sender lock in the consumer), so it
// needs prefetch well above 1 to keep that pool fed; with prefetch=1 the whole
// pipeline serializes behind one slow AI reply. All other topics keep prefetch=1,
// which preserves their existing one-at-a-time semantics.
var prefetchByTopic = map[string]int{
	"webhook.whatsapp.message": 20,
}

const defaultChannelPrefetch = 1

func channelPrefetch(topic string) int {
	if n, ok := prefetchByTopic[topic]; ok && n > 0 {
		return n
	}
	return defaultChannelPrefetch
}

func (q *RabbitMQQueueSub) setupChannel(topic string) (*amqp.Channel, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if old, ok := q.channels[topic]; ok && old != nil {
		_ = old.Close()
	}
	if rel, ok := q.releases[topic]; ok && rel != nil {
		rel()
	}

	ch, release, err := q.pool.OpenChannel()
	if err != nil {
		delete(q.channels, topic)
		delete(q.releases, topic)
		return nil, fmt.Errorf("failed to acquire channel from pool: %w", err)
	}

	if err := ch.Qos(channelPrefetch(topic), 0, false); err != nil {
		_ = ch.Close()
		release()
		return nil, fmt.Errorf("failed to set QoS: %w", err)
	}

	if err := ch.ExchangeDeclare(q.exchange, amqp.ExchangeDirect, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		release()
		return nil, fmt.Errorf("failed to declare exchange: %w", err)
	}

	if _, err := ch.QueueDeclare(topic, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		release()
		return nil, fmt.Errorf("failed to declare queue: %w", err)
	}

	if err := ch.QueueBind(topic, topic, q.exchange, false, nil); err != nil {
		_ = ch.Close()
		release()
		return nil, fmt.Errorf("failed to bind queue: %w", err)
	}

	delayTopic := topic + messaging.DelayQueueSuffix
	delayArgs := amqp.Table{
		"x-dead-letter-exchange":    q.exchange,
		"x-dead-letter-routing-key": topic,
	}
	ch, err = declareDelayQueue(ch, func() (*amqp.Channel, error) {
		// The 406 closed the channel; take a fresh one and hand the old slot back.
		newCh, newRelease, openErr := q.pool.OpenChannel()
		if openErr != nil {
			return nil, openErr
		}
		release()
		release = newRelease
		return newCh, nil
	}, delayTopic, delayArgs)
	if err != nil {
		_ = ch.Close()
		release()
		return nil, fmt.Errorf("failed to declare delay queue: %w", err)
	}
	// Re-assert prefetch: the repair path above may have swapped in a new channel.
	if err := ch.Qos(channelPrefetch(topic), 0, false); err != nil {
		_ = ch.Close()
		release()
		return nil, fmt.Errorf("failed to set QoS: %w", err)
	}
	if err := ch.QueueBind(delayTopic, delayTopic, q.exchange, false, nil); err != nil {
		_ = ch.Close()
		release()
		return nil, fmt.Errorf("failed to bind delay queue: %w", err)
	}

	q.channels[topic] = ch
	q.releases[topic] = release
	return ch, nil
}

func (q *RabbitMQQueueSub) Subscribe(topic string, handler func(message []byte, ack messaging.MessageAck)) error {
	q.mu.Lock()
	delete(q.stopped, topic)
	q.mu.Unlock()

	ch, err := q.setupChannel(topic)
	if err != nil {
		return err
	}

	go q.consumeLoop(topic, ch, handler)
	return nil
}

func (q *RabbitMQQueueSub) StopConsumer(topic string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.stopped[topic] = true

	if ch, ok := q.channels[topic]; ok && ch != nil {
		_ = ch.Close()
		delete(q.channels, topic)
	}
	if rel, ok := q.releases[topic]; ok && rel != nil {
		rel()
		delete(q.releases, topic)
	}

	return nil
}

func (q *RabbitMQQueueSub) isStopped(topic string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.stopped[topic]
}

func (q *RabbitMQQueueSub) consumeLoop(topic string, ch *amqp.Channel, handler func([]byte, messaging.MessageAck)) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if q.isStopped(topic) {

			return
		}

		msgs, err := ch.Consume(topic, "", false, false, false, false, nil)
		if err != nil {
			if q.isStopped(topic) {

				return
			}
			log.Printf("rabbitmq: failed to start consumer for %s: %v, reconnecting in %v", topic, err, backoff)
			time.Sleep(backoff)
			backoff = min(backoff*2, maxBackoff)
			newCh, err := q.setupChannel(topic)
			if err != nil {
				log.Printf("rabbitmq: reconnect failed for %s: %v", topic, err)
				continue
			}
			ch = newCh
			continue
		}

		backoff = time.Second

		for msg := range msgs {
			func(d amqp.Delivery) {
				a := &amqpMessageAck{delivery: d}
				defer func() {
					if r := recover(); r != nil {
						log.Printf("rabbitmq: handler panic for topic %s: %v", topic, r)
						_ = a.Nack(true)
					}
				}()
				handler(d.Body, a)
			}(msg)
		}

		if q.isStopped(topic) {

			return
		}

		time.Sleep(backoff)
		backoff = min(backoff*2, maxBackoff)
		newCh, err := q.setupChannel(topic)
		if err != nil {
			log.Printf("rabbitmq: reconnect failed for %s: %v", topic, err)
			continue
		}
		ch = newCh
	}
}

func (q *RabbitMQQueueSub) DeleteQueue(topic string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.stopped[topic] = true

	delayTopic := topic + messaging.DelayQueueSuffix

	ch, ok := q.channels[topic]
	if !ok || ch == nil {

		tmpCh, tmpRelease, err := q.pool.OpenChannel()
		if err != nil {
			return fmt.Errorf("failed to open channel to delete queue %s: %w", topic, err)
		}
		defer func() {
			_ = tmpCh.Close()
			tmpRelease()
		}()
		_, _ = tmpCh.QueueDelete(delayTopic, false, false, false)
		_, err = tmpCh.QueueDelete(topic, false, false, false)
		return err
	}

	_, _ = ch.QueueDelete(delayTopic, false, false, false)

	_, err := ch.QueueDelete(topic, false, false, false)
	if err != nil {
		log.Printf("rabbitmq: failed to delete queue %s: %v (consumer loop will still exit)", topic, err)
	}

	_ = ch.Close()
	delete(q.channels, topic)
	if rel, ok := q.releases[topic]; ok && rel != nil {
		rel()
		delete(q.releases, topic)
	}
	return err
}

func (q *RabbitMQQueueSub) ValidateConnection() error {
	ch, release, err := q.pool.OpenChannel()
	if err != nil {
		return err
	}
	_ = ch.Close()
	release()
	return nil
}

func (q *RabbitMQQueueSub) GetQueueLength(topic string) (int, error) {
	ch, release, err := q.pool.OpenChannel()
	if err != nil {
		return 0, fmt.Errorf("failed to get channel for queue inspect: %w", err)
	}
	defer func() {
		_ = ch.Close()
		release()
	}()

	info, err := ch.QueueInspect(topic)
	if err != nil {
		return 0, nil
	}
	return info.Messages, nil
}

func (q *RabbitMQQueueSub) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	for topic, ch := range q.channels {
		if ch != nil {
			_ = ch.Close()
		}
		delete(q.channels, topic)
	}
	for topic, rel := range q.releases {
		if rel != nil {
			rel()
		}
		delete(q.releases, topic)
	}
	return nil
}

type amqpMessageAck struct {
	delivery amqp.Delivery
	acked    bool
}

func (a *amqpMessageAck) Ack() error {
	if a.acked {
		return nil
	}
	a.acked = true
	return a.delivery.Ack(false)
}

func (a *amqpMessageAck) Nack(requeue bool) error {
	if a.acked {
		return nil
	}
	a.acked = true
	return a.delivery.Nack(false, requeue)
}

func (a *amqpMessageAck) DeliveryCount() int {

	if a.delivery.Headers == nil {
		return 1
	}
	xDeath, ok := a.delivery.Headers["x-death"]
	if !ok {
		return 1
	}
	deaths, ok := xDeath.([]interface{})
	if !ok || len(deaths) == 0 {
		return 1
	}
	for _, d := range deaths {
		entry, ok := d.(amqp.Table)
		if !ok {
			continue
		}
		if count, ok := entry["count"]; ok {
			switch v := count.(type) {
			case int64:
				return int(v) + 1
			case int32:
				return int(v) + 1
			}
		}
	}
	return 1
}
