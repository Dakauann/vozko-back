package queue

import (
	"testing"

	"vozko/domain/crm_telemetry"
	"vozko/domain/workflow"
)

func TestEveryWorkerPoolTopicPrefetchesExactlyAsManyMessagesAsItHasWorkers(t *testing.T) {
	pools := map[string]int{
		workflow.TopicStageEntered:     workflow.StageEnteredWorkers,
		workflow.TopicAgentRunFinished: workflow.AgentRunFinishedWorkers,
		crm_telemetry.Topic:            crm_telemetry.ConsumerWorkers,
	}
	for topic, workers := range pools {
		if workers < 2 {
			t.Fatalf("%s declares %d workers, a pool needs at least 2 to run anything at once", topic, workers)
		}
		if got := channelPrefetch(topic); got != workers {
			t.Fatalf("%s prefetches %d but runs %d workers: with fewer messages in flight the extra workers are dead configuration", topic, got, workers)
		}
	}
}

func TestChannelPrefetch_DefaultsToOne(t *testing.T) {
	if got := channelPrefetch("some.other.topic"); got != 1 {
		t.Fatalf("unknown topic must default to prefetch 1, got %d", got)
	}
	if got := channelPrefetch(""); got != 1 {
		t.Fatalf("empty topic must default to prefetch 1, got %d", got)
	}
}

func TestChannelPrefetch_WhatsAppMessageOverride(t *testing.T) {
	got := channelPrefetch("webhook.whatsapp.message")
	if got <= 1 {
		t.Fatalf("webhook.whatsapp.message must use prefetch > 1 for concurrency, got %d", got)
	}
}
