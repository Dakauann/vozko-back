package lead_memory

import (
	"context"

	"vozko/domain/actor"
)

// WriteActor is who is performing a mutation. Handlers build it from JWT
// claims; the AI tool builds it with actor.FormatAI(agentID); anything else is
// the system actor.
type WriteActor struct {
	Kind actor.Kind
	ID   string
}

// CreateInput is a request to remember one fact about a lead.
type CreateInput struct {
	WorkspaceID string
	LeadID      string
	Content     string
	Category    Category

	Actor WriteActor

	// Source entry, when the write happens inside a conversation. Feeds
	// provenance columns and the CRM timeline event.
	SourceEntryID   *string
	SourceEntryType *string
}

// CreateResult carries the memory plus whether it already existed. A duplicate
// create is idempotent success, not an error: the caller answers with the
// existing row and nothing was written.
type CreateResult struct {
	Memory       *LeadMemory
	Deduplicated bool
}

type CreateUseCase interface {
	Execute(ctx context.Context, in CreateInput) (*CreateResult, error)
}

// UpdateInput edits a memory's content and/or category.
//
// MemoryRef is either a full UUID or a ≥MinIDPrefixLen prefix (the form the
// prompt block shows the model). Prefix resolution requires LeadID, because a
// prefix is only unique within one lead's memories; the HTTP surface always
// passes full ids and may leave LeadID empty.
type UpdateInput struct {
	WorkspaceID string
	LeadID      string
	MemoryRef   string

	// Content and Category empty mean "keep".
	Content  string
	Category Category

	Actor WriteActor

	SourceEntryID   *string
	SourceEntryType *string
}

type UpdateUseCase interface {
	Execute(ctx context.Context, in UpdateInput) (*LeadMemory, error)
}

// DeleteInput forgets a memory (soft delete). Same reference semantics as
// UpdateInput.
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

// MemoryView is a memory plus its resolved display attribution. ActorLabel is
// best-effort ("Maria Souza", "Agente Vendas SP") and empty when resolution
// fails; consumers fall back to the kind.
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
