package delayedfire

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"vozko/domain/messaging"
)

type published struct {
	topic   string
	payload []byte
	delay   time.Duration
	delayed bool
}

type fakePub struct{ sent []published }

func (f *fakePub) Publish(topic string, message []byte) error {
	f.sent = append(f.sent, published{topic: topic, payload: message})
	return nil
}

func (f *fakePub) PublishWithDelay(topic string, message []byte, delay time.Duration) error {
	f.sent = append(f.sent, published{topic: topic, payload: message, delay: delay, delayed: true})
	return nil
}

func (f *fakePub) ValidateConnection() error { return nil }

type fakeSub struct {
	topic   string
	handler func([]byte, messaging.MessageAck)
}

func (f *fakeSub) Subscribe(topic string, handler func([]byte, messaging.MessageAck)) error {
	f.topic, f.handler = topic, handler
	return nil
}
func (f *fakeSub) DeleteQueue(string) error           { return nil }
func (f *fakeSub) ValidateConnection() error          { return nil }
func (f *fakeSub) GetQueueLength(string) (int, error) { return 0, nil }

type fakeAck struct{ acked int }

func (a *fakeAck) Ack() error         { a.acked++; return nil }
func (a *fakeAck) Nack(bool) error    { return nil }
func (a *fakeAck) DeliveryCount() int { return 1 }

func TestTheSchedulerDelaysFarFiresAndPublishesNearOnesAtOnce(t *testing.T) {
	pub := &fakePub{}
	scheduler, err := NewScheduler(pub, "topic.fire")
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.ScheduleFire("far", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.ScheduleFire("near", time.Now().Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(pub.sent) != 2 || !pub.sent[0].delayed || pub.sent[1].delayed {
		t.Fatalf("sent = %+v", pub.sent)
	}
	if pub.sent[0].topic != "topic.fire" || pub.sent[0].delay < 59*time.Minute {
		t.Fatalf("far = %+v", pub.sent[0])
	}
	var msg Message
	if err := json.Unmarshal(pub.sent[0].payload, &msg); err != nil || msg.ID != "far" || msg.FireAt == 0 {
		t.Fatalf("payload = %s, %v", pub.sent[0].payload, err)
	}
}

func TestNewSchedulerRefusesMissingParts(t *testing.T) {
	if _, err := NewScheduler(nil, "topic"); err == nil {
		t.Fatal("a scheduler without a publisher must not be built")
	}
	if _, err := NewScheduler(&fakePub{}, " "); err == nil {
		t.Fatal("a scheduler without a topic must not be built")
	}
}

func TestTheConsumerFiresEachIDAndAcksEveryDelivery(t *testing.T) {
	sub := &fakeSub{}
	fired := []string{}
	consumer, err := NewConsumer(sub, "topic.fire", "test", func(_ context.Context, id string) error {
		fired = append(fired, id)
		return errors.New("logged, never retried by the queue")
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer.Start(); err != nil || sub.topic != "topic.fire" {
		t.Fatalf("subscribed to %q, %v", sub.topic, err)
	}
	for _, payload := range []string{`{"id":"a","fire_at":1}`, `not json`, `{"id":"  "}`, `{"id":"b"}`} {
		ack := &fakeAck{}
		sub.handler([]byte(payload), ack)
		if ack.acked != 1 {
			t.Fatalf("payload %s acked %d times", payload, ack.acked)
		}
	}
	if len(fired) != 2 || fired[0] != "a" || fired[1] != "b" {
		t.Fatalf("fired = %v", fired)
	}
}

func TestNewConsumerRefusesMissingParts(t *testing.T) {
	fire := func(context.Context, string) error { return nil }
	if _, err := NewConsumer(nil, "topic", "test", fire); err == nil {
		t.Fatal("no subscriber")
	}
	if _, err := NewConsumer(&fakeSub{}, "topic", "test", nil); err == nil {
		t.Fatal("no fire function")
	}
}
