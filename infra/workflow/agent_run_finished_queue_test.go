package workflow_infra

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"vozko/domain/workflow"
)

type capturingPub struct {
	topic   string
	payload []byte
	err     error
}

func (p *capturingPub) Publish(topic string, message []byte) error {
	p.topic, p.payload = topic, message
	return p.err
}

func (p *capturingPub) PublishWithDelay(string, []byte, time.Duration) error { return nil }
func (p *capturingPub) ValidateConnection() error                            { return nil }

func TestAFinishedAgentRunIsQueuedOnItsTopicAsJSON(t *testing.T) {
	pub := &capturingPub{}
	ev := workflow.AgentRunFinished{RunID: "run-1", EntryID: "e-1", EntryType: "whatsapp", AgentID: "agent-7", Status: workflow.RunStatusCompleted, HasAgentResult: true}

	if err := NewAgentRunFinishedQueue(pub).PublishAgentRunFinished(ev); err != nil {
		t.Fatal(err)
	}

	if pub.topic != workflow.TopicAgentRunFinished {
		t.Fatalf("topic = %q", pub.topic)
	}
	var got workflow.AgentRunFinished
	if err := json.Unmarshal(pub.payload, &got); err != nil || got != ev {
		t.Fatalf("payload = %s (%v)", pub.payload, err)
	}
}

func TestAQueueFailureReachesTheEngine(t *testing.T) {
	if err := NewAgentRunFinishedQueue(&capturingPub{err: errors.New("broker down")}).PublishAgentRunFinished(workflow.AgentRunFinished{}); err == nil {
		t.Fatal("the engine must learn the event was lost")
	}
	if err := NewAgentRunFinishedQueue(nil).PublishAgentRunFinished(workflow.AgentRunFinished{}); err == nil {
		t.Fatal("an unwired queue must refuse")
	}
}
