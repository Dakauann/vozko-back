package conversationnote

import (
	"context"
	"time"

	"vozko/domain/shared"
)

type PageQuery struct {
	WorkspaceID string
	Entry       shared.EntryRef
	Before      *time.Time
	Limit       int
}

type Repository interface {
	Create(ctx context.Context, note Note) error
	Find(ctx context.Context, workspaceID, id string) (Note, error)
	ListByEntry(ctx context.Context, q PageQuery) ([]Note, error)
	MarkDeleted(ctx context.Context, workspaceID, id string, at time.Time) error
}

type Authored struct {
	Note
	AuthorName string
	Images     []Image
}

type ImageUpload struct {
	WorkspaceID string
	Entry       shared.EntryRef
	FileName    string
	Data        []byte
}

type Page struct {
	Notes   []Authored
	HasMore bool
}

type UseCases interface {
	List(ctx context.Context, by shared.Person, q PageQuery) (Page, error)
	Create(ctx context.Context, by shared.Person, d Draft) (Authored, error)
	AttachImage(ctx context.Context, by shared.Person, in ImageUpload) (Image, error)
	Delete(ctx context.Context, by shared.Person, workspaceID, noteID string) error
}
