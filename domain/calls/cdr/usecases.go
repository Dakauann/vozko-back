package cdr

import (
	"time"

	"vozko/domain/shared"
)

type StartCallInput struct {
	CallID         string
	WorkspaceID    string
	Type           CallType
	Direction      Direction
	Source         Source
	ConversationID *string
	LeadID         *string
	PhoneFrom      string
	PhoneTo        string
	TrunkID        *string
	AgentID        *string
	ParentCallID   *string
	StartedAt      time.Time
}

type CompleteCallInput struct {
	CallID    string
	Status    Status
	EndedAt   time.Time
	EndReason *string
}

type StartCallUseCase interface {
	Execute(input StartCallInput) (*Call, error)
}

type MarkCallAnsweredUseCase interface {
	Execute(callID string, answeredAt time.Time) error
}

type CompleteCallUseCase interface {
	Execute(input CompleteCallInput) error
}

type GetCallUseCase interface {
	Execute(callID string) (*Call, error)
}

type ListCallsUseCase interface {
	Execute(filters ListFilters) (*shared.PaginatedResult[*Call], error)
}
