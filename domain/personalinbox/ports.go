package personalinbox

import (
	"context"

	"vozko/domain/shared"
)

type Change func(Mark) (Mark, error)

type Repository interface {
	Find(ctx context.Context, userID string, entry shared.EntryRef) (Mark, error)
	Change(ctx context.Context, userID string, entry shared.EntryRef, change Change) (before, after Mark, err error)
	ForEntries(ctx context.Context, userID string, entries []shared.EntryRef) ([]Mark, error)
}

type UseCases interface {
	Pin(ctx context.Context, by shared.Person, workspaceID string, entry shared.EntryRef, pinned bool) (Mark, error)
	MarkUnread(ctx context.Context, by shared.Person, workspaceID string, entry shared.EntryRef) (Mark, error)
	ClearUnreadMark(ctx context.Context, by shared.Person, workspaceID string, entry shared.EntryRef) (Mark, error)
	MarkRead(ctx context.Context, by shared.Person, workspaceID string, entry shared.EntryRef)
	PageMarks(ctx context.Context, userID string, entries []shared.EntryRef) []Mark
}
