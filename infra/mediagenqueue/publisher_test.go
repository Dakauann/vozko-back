package mediagenqueue

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"vozko/domain/mediagen"
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

func TestEnqueuePublishesTheJobOnItsKindsTopic(t *testing.T) {
	for kind, topic := range map[mediagen.Kind]string{mediagen.KindImage: mediagen.Topic, mediagen.KindMusic: mediagen.Topic, mediagen.KindVideo: mediagen.RenderTopic} {
		pub := &recordingPub{}
		if err := NewPublisher(pub).Enqueue(&mediagen.Job{ID: "job-1", Kind: kind}); err != nil {
			t.Fatal(err)
		}
		var msg mediagen.QueueMessage
		if err := json.Unmarshal(pub.body, &msg); err != nil || msg.JobID != "job-1" || pub.topic != topic {
			t.Fatalf("%s: topic %s body %s err %v", kind, pub.topic, pub.body, err)
		}
	}
	if err := NewPublisher(&recordingPub{}).Enqueue(&mediagen.Job{ID: "job-1", Kind: "gif"}); err == nil {
		t.Fatal("an unknown kind was published")
	}
}

func TestEnqueueReportsAPublishFailure(t *testing.T) {
	pub := &recordingPub{err: errors.New("rabbit down")}
	if err := NewPublisher(pub).Enqueue(&mediagen.Job{ID: "job-1", Kind: mediagen.KindImage}); err == nil {
		t.Fatal("publish failure hidden")
	}
}
