package mediagenqueue

import (
	"encoding/json"
	"fmt"

	"vozko/domain/mediagen"
	"vozko/domain/messaging"
)

type Publisher struct {
	pub messaging.MessageQueuePub
}

var _ mediagen.Queue = (*Publisher)(nil)

func NewPublisher(pub messaging.MessageQueuePub) *Publisher {
	return &Publisher{pub: pub}
}

func (p *Publisher) Enqueue(job *mediagen.Job) error {
	if job == nil || !job.Kind.Known() {
		return fmt.Errorf("mediagenqueue: a job of a known kind is required")
	}
	payload, err := json.Marshal(mediagen.QueueMessage{JobID: job.ID})
	if err != nil {
		return err
	}
	return p.pub.Publish(job.Kind.Topic(), payload)
}
