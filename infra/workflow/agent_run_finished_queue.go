package workflow_infra

import (
	"encoding/json"
	"errors"
	"fmt"

	"vozko/domain/messaging"
	"vozko/domain/workflow"
)

var errAgentRunQueueMissing = errors.New("workflow agent run queue: no publisher wired")

type AgentRunFinishedQueue struct {
	pub messaging.MessageQueuePub
}

func NewAgentRunFinishedQueue(pub messaging.MessageQueuePub) *AgentRunFinishedQueue {
	return &AgentRunFinishedQueue{pub: pub}
}

func (q *AgentRunFinishedQueue) PublishAgentRunFinished(event workflow.AgentRunFinished) error {
	if q == nil || q.pub == nil {
		return errAgentRunQueueMissing
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode agent run %s end: %w", event.RunID, err)
	}
	return q.pub.Publish(workflow.TopicAgentRunFinished, payload)
}

var _ workflow.AgentRunFinishedPublisher = (*AgentRunFinishedQueue)(nil)
