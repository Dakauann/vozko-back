package lead_memory

import (
	"vozko/domain/agent"
	ce "vozko/domain/conversation_event"
	"vozko/domain/user"
)

// TimelineLogger records memory mutations on the CRM conversation timeline.
// Best-effort by contract: implementations never return errors and never fail
// the mutation they annotate. Satisfied by *crm_telemetry_usecase.Emitter.
type TimelineLogger interface {
	ConversationEvent(ev *ce.ConversationEvent)
}

// AgentNameFinder and UserNameFinder are the narrow slices of the agent and
// user repositories the listing needs to label writers ("Agente Vendas SP",
// "Maria"). Declared here so the use case does not drag two fat repository
// interfaces in for one cosmetic lookup. Satisfied by agent.Repository and
// user.UserRepository respectively.
type AgentNameFinder interface {
	FindByIDs(agentIDs []string) ([]*agent.Agent, error)
}

type UserNameFinder interface {
	FindByIDs(userIds []string) ([]*user.User, error)
}
