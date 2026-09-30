package callrouting_infra

import (
	"sync"
	"time"

	"vozko/domain/callrouting"
)

type AgentActivity struct {
	mu     sync.RWMutex
	agents map[string]callrouting.AgentStats
}

var _ callrouting.AgentActivity = (*AgentActivity)(nil)

func NewAgentActivity() *AgentActivity {
	return &AgentActivity{agents: map[string]callrouting.AgentStats{}}
}

func agentKey(workspaceID, userID string) string {
	return workspaceID + "|" + userID
}

func (a *AgentActivity) Stats(workspaceID string, userIDs []string) []callrouting.AgentStats {
	a.mu.RLock()
	defer a.mu.RUnlock()
	stats := make([]callrouting.AgentStats, 0, len(userIDs))
	for _, userID := range userIDs {
		agent := a.agents[agentKey(workspaceID, userID)]
		agent.UserID = userID
		stats = append(stats, agent)
	}
	return stats
}

func (a *AgentActivity) CallEnded(workspaceID, userID string, at time.Time) {
	a.update(workspaceID, userID, func(agent *callrouting.AgentStats) { agent.LastCallEndedAt = at })
}

func (a *AgentActivity) CallAnswered(workspaceID, userID string) {
	a.update(workspaceID, userID, func(agent *callrouting.AgentStats) { agent.CallsAnswered++ })
}

func (a *AgentActivity) update(workspaceID, userID string, change func(*callrouting.AgentStats)) {
	if workspaceID == "" || userID == "" {
		return
	}
	key := agentKey(workspaceID, userID)
	a.mu.Lock()
	agent := a.agents[key]
	change(&agent)
	a.agents[key] = agent
	a.mu.Unlock()
}
