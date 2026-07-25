package messaging

import "time"

const MaxRetries = 3

const DelayQueueSuffix = ".delay"

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
