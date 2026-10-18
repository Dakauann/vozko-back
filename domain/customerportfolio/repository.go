package customerportfolio

import (
	"context"

	"vozko/domain/shared"
)

type Repository interface {
	Insert(ctx context.Context, e Entry) (Entry, bool, error)
	Get(ctx context.Context, workspaceID, id string) (Entry, error)
	FindBySubject(ctx context.Context, workspaceID string, s Subject) (*Entry, error)
	SaveHolder(ctx context.Context, e Entry) error
	Delete(ctx context.Context, workspaceID, id string) error
	List(ctx context.Context, workspaceID string, q ListQuery) (*shared.PaginatedResult[Row], error)
}

type ContactResolver interface {
	ContactOfConversation(ctx context.Context, workspaceID string, ref shared.EntryRef) (Subject, error)
}
