package webchat

import (
	"context"
	"time"

	"vozko/domain/agent"
	"vozko/domain/cache"
	"vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/lead"
	"vozko/domain/pipeline"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	wd "vozko/domain/workspace/workspace_department"
	conversation_usecase "vozko/usecases/conversation"
)

type Assignments interface {
	EnsureAssignment(entryID, entryType, accountID string) string
	HandOffToRoulette(in ia.RouletteHandOff) (string, error)
}

type Automation interface {
	Dispatch(ctx context.Context, in conversation_usecase.InboundAutomationInput)
}

type LeadFinder interface {
	FindOrCreate(workspaceID, number string, update lead.LeadUpdate) (*lead.Lead, bool, error)
}

type OneShot interface {
	SetNX(key string, value string, ttl time.Duration) (bool, error)
	Del(keys ...string) error
}

type MessageReader interface {
	ListByEntryPaginated(input conversation.ListMessagesInput) ([]*conversation.Message, error)
}

type MessagePresenter interface {
	PresentMessages(entryID string, entryType shared.EntryType, messages []*conversation.Message)
}

type OperatorNotifier interface {
	BroadcastTyping(entryID, entryType, fromUserID string, isTyping bool)
	BroadcastEntryUpdate(entryID, entryType string, message *conversation.Message)
}

type AgentFinder interface {
	FindByID(agentID string) (*agent.Agent, error)
}

type WorkflowFinder interface {
	FindByID(workflowID string) (*workflow.Workflow, error)
}

type PipelineFinder interface {
	GetByID(workspaceID, id string) (*pipeline.Pipeline, error)
}

type DepartmentFinder interface {
	GetDepartmentByID(id string) (*wd.Department, error)
}

type Limits struct {
	SessionsPerIP         cache.RateLimiter
	SessionsPerWidget     cache.RateLimiter
	MessagesPerVisitor    cache.RateLimiter
	MessagesPerVisitorDay cache.RateLimiter
	MessagesPerIP         cache.RateLimiter
	UploadsPerVisitor     cache.RateLimiter
	HandOffsPerVisitor    cache.RateLimiter
	TypingPerVisitor      cache.RateLimiter
}
