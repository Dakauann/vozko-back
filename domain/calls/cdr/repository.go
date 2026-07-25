package cdr

import (
	"time"

	"vozko/domain/shared"
)

type Repository interface {
	Create(call *Call) error
	GetByID(id string) (*Call, error)
	GetByCallID(callID string) (*Call, error)
	Update(call *Call) error
	MarkAnswered(callID string, answeredAt time.Time) error
	Complete(input CompleteInput) error
	List(filters ListFilters) (*shared.PaginatedResult[*Call], error)
}

type CompleteInput struct {
	CallID      string
	Status      Status
	EndedAt     *time.Time
	DurationSec int
	EndReason   *string
}
