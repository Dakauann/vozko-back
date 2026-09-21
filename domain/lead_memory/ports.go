package lead_memory

import (
	"vozko/domain/agent"
	ce "vozko/domain/conversation_event"
	"vozko/domain/user"
)

type TimelineLogger interface {
	ConversationEvent(ev *ce.ConversationEvent)
}

type AgentNameFinder interface {
	FindByIDs(agentIDs []string) ([]*agent.Agent, error)
}

type UserNameFinder interface {
	FindByIDs(userIds []string) ([]*user.User, error)
}
