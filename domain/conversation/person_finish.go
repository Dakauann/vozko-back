package conversation

import (
	"context"
	"errors"

	"vozko/domain/shared"
)

var ErrStatusChangeForbidden = errors.New("conversation: changing the status needs conversations:send")

type PersonFinishUseCase interface {
	Finish(ctx context.Context, by shared.Person, workspaceID string, entry shared.EntryRef, outcomeCode string) error
}
