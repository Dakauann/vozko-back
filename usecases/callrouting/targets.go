package callrouting_usecase

import (
	"context"

	"vozko/domain/callrouting"
)

type QueueTarget struct {
	ID      string
	Name    string
	Waiting int
	Ready   int
}

type TransferTargets struct {
	queues     callrouting.QueueRepository
	dispatcher *Dispatcher
}

func NewTransferTargets(queues callrouting.QueueRepository, dispatcher *Dispatcher) *TransferTargets {
	return &TransferTargets{queues: queues, dispatcher: dispatcher}
}

func (t *TransferTargets) Queues(ctx context.Context, workspaceID string) ([]QueueTarget, error) {
	queues, err := t.queues.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	targets := make([]QueueTarget, 0, len(queues))
	for _, queue := range queues {
		live, err := t.dispatcher.Live(ctx, queue)
		if err != nil {
			return nil, err
		}
		targets = append(targets, QueueTarget{ID: queue.ID, Name: queue.Name, Waiting: len(live.Waiting), Ready: live.AgentCounts()[callrouting.AgentFree]})
	}
	return targets, nil
}
