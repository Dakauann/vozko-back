package lead_memory

import (
	"context"

	"vozko/domain/actor"
)

type WriteActor struct {
	Kind actor.Kind
	ID   string
}

type CreateInput struct {
	WorkspaceID string
	LeadID      string
	Content     string
	Category    Category

	Actor WriteActor

	SourceEntryID   *string
	SourceEntryType *string
}

type CreateResult struct {
	Memory       *LeadMemory
	Deduplicated bool
}

type CreateUseCase interface {
	Execute(ctx context.Context, in CreateInput) (*CreateResult, error)
}

type UpdateInput struct {
	WorkspaceID string
	LeadID      string
	MemoryRef   string

	Content  string
	Category Category

	Actor WriteActor

	SourceEntryID   *string
	SourceEntryType *string
}

type UpdateUseCase interface {
	Execute(ctx context.Context, in UpdateInput) (*LeadMemory, error)
}

type DeleteInput struct {
	WorkspaceID string
	LeadID      string
	MemoryRef   string

	Actor WriteActor

	SourceEntryID   *string
	SourceEntryType *string
}

type DeleteUseCase interface {
	Execute(ctx context.Context, in DeleteInput) error
}

type MemoryView struct {
	*LeadMemory
	ActorLabel string `json:"actorLabel,omitempty"`
}

type ListInput struct {
	WorkspaceID string
	LeadID      string
	Query       ListQuery
}

type ListResult struct {
	Items []MemoryView
	Total int64
}

type ListUseCase interface {
	Execute(ctx context.Context, in ListInput) (*ListResult, error)
}
