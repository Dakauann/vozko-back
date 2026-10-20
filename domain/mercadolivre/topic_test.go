package mercadolivre

import (
	"errors"
	"testing"

	"vozko/domain/webhook"
)

func TestEveryJobKindHasAQueueTopic(t *testing.T) {
	cases := map[JobKind]string{
		JobFetchMessage:  webhook.TopicMercadoLivreMessage,
		JobFetchThread:   webhook.TopicMercadoLivreMessage,
		JobFetchQuestion: webhook.TopicMercadoLivreQuestion,
		JobFetchItem:     webhook.TopicMercadoLivreQuestion,
		JobFetchOrder:    webhook.TopicMercadoLivreOrder,
		JobFetchShipment: webhook.TopicMercadoLivreOrder,
		JobRefreshPack:   webhook.TopicMercadoLivreOrder,
	}
	for kind, want := range cases {
		got, err := TopicForJob(kind)
		if err != nil || got != want {
			t.Errorf("%s -> %q, %v; want %q", kind, got, err, want)
		}
	}
	for _, kind := range JobKinds() {
		if _, ok := cases[kind]; !ok {
			t.Errorf("job kind %s has no expected topic in this test", kind)
		}
	}
	if _, err := TopicForJob("fetch_claim"); !errors.Is(err, ErrUnknownJobKind) {
		t.Fatalf("unknown kind = %v", err)
	}
}

func TestTopicFamiliesAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, topic := range QueueTopics() {
		if seen[topic] {
			t.Fatalf("%s listed twice", topic)
		}
		seen[topic] = true
	}
	if len(seen) != 3 {
		t.Fatalf("topics = %v", QueueTopics())
	}
}
