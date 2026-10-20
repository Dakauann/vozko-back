package delayedfire

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/messaging"
)

const immediateFireThreshold = 10 * time.Second

type Message struct {
	ID     string `json:"id"`
	FireAt int64  `json:"fire_at"`
}

type Scheduler struct {
	pub   messaging.MessageQueuePub
	topic string
}

func NewScheduler(pub messaging.MessageQueuePub, topic string) (*Scheduler, error) {
	if pub == nil {
		return nil, fmt.Errorf("delayed fire scheduler: missing queue publisher")
	}
	if strings.TrimSpace(topic) == "" {
		return nil, fmt.Errorf("delayed fire scheduler: missing topic")
	}
	return &Scheduler{pub: pub, topic: topic}, nil
}

func (s *Scheduler) ScheduleFire(id string, fireAt time.Time) error {
	payload, err := json.Marshal(Message{ID: id, FireAt: fireAt.UTC().UnixMilli()})
	if err != nil {
		return err
	}
	delay := time.Until(fireAt)
	if delay <= immediateFireThreshold {
		log.Printf("[%s] %s fires in %s, publishing immediately", s.topic, id, delay)
		return s.pub.Publish(s.topic, payload)
	}
	return s.pub.PublishWithDelay(s.topic, payload, delay)
}

type Consumer struct {
	sub   messaging.MessageQueueSub
	topic string
	label string
	fire  func(ctx context.Context, id string) error
}

func NewConsumer(sub messaging.MessageQueueSub, topic, label string, fire func(ctx context.Context, id string) error) (*Consumer, error) {
	missing := []string{}
	if sub == nil {
		missing = append(missing, "queue subscriber")
	}
	if fire == nil {
		missing = append(missing, "fire function")
	}
	if strings.TrimSpace(topic) == "" {
		missing = append(missing, "topic")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s fire consumer: missing %s", label, strings.Join(missing, ", "))
	}
	return &Consumer{sub: sub, topic: topic, label: label, fire: fire}, nil
}

func (c *Consumer) Start() error {
	return c.sub.Subscribe(c.topic, c.handle)
}

func (c *Consumer) handle(payload []byte, ack messaging.MessageAck) {
	defer func() {
		if err := ack.Ack(); err != nil {
			log.Printf("[%s] could not ack a fire message: %v", c.label, err)
		}
	}()
	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		log.Printf("[%s] discarding an unparseable fire message: %v", c.label, err)
		return
	}
	id := strings.TrimSpace(msg.ID)
	if id == "" {
		log.Printf("[%s] discarding a fire message with no id", c.label)
		return
	}
	if err := c.fire(context.Background(), id); err != nil {
		log.Printf("[%s] dispatch of %s failed: %v", c.label, id, err)
	}
}
