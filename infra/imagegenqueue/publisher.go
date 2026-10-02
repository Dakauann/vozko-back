package imagegenqueue

import (
	"encoding/json"

	"vozko/domain/imagegen"
	"vozko/domain/messaging"
)

type Publisher struct {
	pub messaging.MessageQueuePub
}

var _ imagegen.Queue = (*Publisher)(nil)

func NewPublisher(pub messaging.MessageQueuePub) *Publisher {
	return &Publisher{pub: pub}
}

func (p *Publisher) Enqueue(jobID string) error {
	payload, err := json.Marshal(imagegen.QueueMessage{JobID: jobID})
	if err != nil {
		return err
	}
	return p.pub.Publish(imagegen.Topic, payload)
}
