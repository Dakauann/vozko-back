package apikey

import (
	"context"
	"time"
)

type Repository interface {
	Create(ctx context.Context, k *Key) error
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Key, error)
	GetByLookupID(ctx context.Context, lookupID string) (*Key, error)
	Get(ctx context.Context, workspaceID, id string) (*Key, error)
	Revoke(ctx context.Context, workspaceID, id, by string, at time.Time) (*Key, error)
	Touch(ctx context.Context, id string, at time.Time) error
	CountActive(ctx context.Context, workspaceID string) (int64, error)
}
