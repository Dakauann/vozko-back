package aibilling

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/ai"
)

type flakyQueue struct {
	failures int
	topics   []string
	bodies   [][]byte
	attempts int
}

func (q *flakyQueue) Publish(topic string, message []byte) error {
	q.attempts++
	if q.attempts <= q.failures {
		return errors.New("broker down")
	}
	q.topics = append(q.topics, topic)
	q.bodies = append(q.bodies, message)
	return nil
}
func (q *flakyQueue) PublishWithDelay(string, []byte, time.Duration) error { return nil }
func (q *flakyQueue) ValidateConnection() error                            { return nil }

func instant(queue *flakyQueue) *Publisher {
	return &Publisher{queue: queue, delays: []time.Duration{0, 0, 0}}
}

func TestACompletedCallIsPublishedAsOneBillingEvent(t *testing.T) {
	queue := &flakyQueue{}
	instant(queue).Publish("ws-1", "typesafe/jev-1.13", 2185, 0, 92)

	if len(queue.bodies) != 1 || queue.topics[0] != ai.TopicAIBillingCompleted {
		t.Fatalf("published %d events on %v", len(queue.bodies), queue.topics)
	}
	var event ai.AICompletedEvent
	if err := json.Unmarshal(queue.bodies[0], &event); err != nil {
		t.Fatal(err)
	}
	if event.RequestID == "" || event.WorkspaceID != "ws-1" || event.Model != "typesafe/jev-1.13" ||
		event.PromptTokens != 2185 || event.CompletionTokens != 0 || event.ProviderCostMicros != 92 {
		t.Fatalf("event = %+v", event)
	}
}

func TestABrokerHiccupIsRetriedAndNeverBillsTwice(t *testing.T) {
	queue := &flakyQueue{failures: 2}
	instant(queue).Publish("ws-1", "m", 10, 1, 5)
	if queue.attempts != 3 || len(queue.bodies) != 1 {
		t.Fatalf("attempts=%d published=%d, want 3 and 1", queue.attempts, len(queue.bodies))
	}
}

func TestAPersistentOutageGivesUpAfterTheLastAttempt(t *testing.T) {
	queue := &flakyQueue{failures: 10}
	instant(queue).Publish("ws-1", "m", 10, 1, 5)
	if queue.attempts != 3 || len(queue.bodies) != 0 {
		t.Fatalf("attempts=%d published=%d, want 3 and 0", queue.attempts, len(queue.bodies))
	}
}

func TestProviderCostIsRoundedUpToWholeMicros(t *testing.T) {
	cases := map[float64]int64{0: 0, -1: 0, 0.000021588: 22, 0.0123: 12300}
	for cost, want := range cases {
		if got := CostToMicros(cost); got != want {
			t.Fatalf("CostToMicros(%v) = %d, want %d", cost, got, want)
		}
	}
}

func TestAChargeForAThreadCarriesTheThreadInItsReference(t *testing.T) {
	queue := &flakyQueue{}
	instant(queue).PublishFor("aichat:th-1", "ws-1", "anthropic/claude", 100, 20, 300)
	var event ai.AICompletedEvent
	if err := json.Unmarshal(queue.bodies[0], &event); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(event.RequestID, "aichat:th-1:") || len(event.RequestID) <= len("aichat:th-1:") {
		t.Fatalf("the request id must sit under the thread reference, got %q", event.RequestID)
	}
	instant(queue).PublishFor("aichat:th-1", "ws-1", "anthropic/claude", 100, 20, 300)
	var second ai.AICompletedEvent
	_ = json.Unmarshal(queue.bodies[1], &second)
	if second.RequestID == event.RequestID {
		t.Fatal("every call must keep its own reference")
	}
}
