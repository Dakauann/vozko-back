package messaging

import "time"

const MaxRetries = 3

const DelayQueueSuffix = ".delay"

// DLQSuffix names the dead-letter queue for a topic.
//
// Retries that exhaust MaxRetries are otherwise logged and dropped, which loses
// the event with no way to inspect or replay it. Publishing to <topic>.dlq keeps
// a parked copy. Channels adopt this as they migrate; Instagram uses it from the
// start.
const DLQSuffix = ".dlq"

type MessageAck interface {
	Ack() error
	Nack(requeue bool) error

	DeliveryCount() int
}

type MessageQueueSub interface {
	Subscribe(topic string, handler func(message []byte, ack MessageAck)) error
	DeleteQueue(topic string) error
	ValidateConnection() error
	// TODO: fix, this is returning as empty, even though some entities havent been processed

	GetQueueLength(topic string) (int, error)
}

type MessageQueuePub interface {
	Publish(topic string, message []byte) error

	PublishWithDelay(topic string, message []byte, delay time.Duration) error
	ValidateConnection() error
}
