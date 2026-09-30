package callrouting_usecase

import (
	"context"
	"time"

	"vozko/domain/callrouting"
)

const maxStatsWindow = 31 * 24 * time.Hour

type QueueMonitorDeps struct {
	Queues     callrouting.QueueRepository
	Dispatcher *Dispatcher
	Names      callrouting.MemberNames
	History    callrouting.QueueHistory
}

type QueueMonitor struct {
	deps QueueMonitorDeps
}

func NewQueueMonitor(deps QueueMonitorDeps) *QueueMonitor {
	return &QueueMonitor{deps: deps}
}

func (m *QueueMonitor) Live(ctx context.Context, workspaceID string) ([]callrouting.QueueLive, error) {
	queues, err := m.deps.Queues.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	live := make([]callrouting.QueueLive, 0, len(queues))
	var userIDs []string
	for _, queue := range queues {
		snapshot, err := m.deps.Dispatcher.Live(ctx, queue)
		if err != nil {
			return nil, err
		}
		for _, agent := range snapshot.Agents {
			userIDs = append(userIDs, agent.UserID)
		}
		live = append(live, snapshot)
	}
	m.name(live, userIDs)
	return live, nil
}

func (m *QueueMonitor) Stats(ctx context.Context, workspaceID string, from, to time.Time) ([]callrouting.QueueTally, error) {
	if !to.After(from) || to.Sub(from) > maxStatsWindow {
		return nil, callrouting.ErrInvalidStatsWindow
	}
	queues, err := m.deps.Queues.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	outcomes, err := m.deps.History.QueueOutcomes(ctx, workspaceID, from, to)
	if err != nil {
		return nil, err
	}
	tallies := make([]callrouting.QueueTally, len(queues))
	index := make(map[string]int, len(queues))
	for i, queue := range queues {
		tallies[i].QueueID = queue.ID
		index[queue.ID] = i
	}
	for _, outcome := range outcomes {
		if i, known := index[outcome.QueueID]; known {
			tallies[i].Count(outcome.Outcome, outcome.Wait)
		}
	}
	return tallies, nil
}

func (m *QueueMonitor) name(live []callrouting.QueueLive, userIDs []string) {
	if m.deps.Names == nil || len(userIDs) == 0 {
		return
	}
	names := m.deps.Names.ResolveUsernames(userIDs)
	for i := range live {
		for j := range live[i].Agents {
			live[i].Agents[j].Name = names[live[i].Agents[j].UserID]
		}
	}
}
