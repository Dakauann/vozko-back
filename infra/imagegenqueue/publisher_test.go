package imagegenqueue

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"vozko/domain/imagegen"
)

type recordingPub struct {
	topic string
	body  []byte
	err   error
}

func (p *recordingPub) Publish(topic string, message []byte) error {
	p.topic, p.body = topic, message
	return p.err
}

func (p *recordingPub) PublishWithDelay(string, []byte, time.Duration) error { return nil }
func (p *recordingPub) ValidateConnection() error                            { return nil }

func TestEnqueuePublishesTheJobOnTheImageTopic(t *testing.T) {
	pub := &recordingPub{}
	if err := NewPublisher(pub).Enqueue("job-1"); err != nil {
		t.Fatal(err)
	}
	var msg imagegen.QueueMessage
	if err := json.Unmarshal(pub.body, &msg); err != nil || msg.JobID != "job-1" || pub.topic != imagegen.Topic {
		t.Fatalf("topic %s body %s err %v", pub.topic, pub.body, err)
	}
}

func TestEnqueueReportsAPublishFailure(t *testing.T) {
	pub := &recordingPub{err: errors.New("rabbit down")}
	if err := NewPublisher(pub).Enqueue("job-1"); err == nil {
		t.Fatal("publish failure hidden")
	}
}
