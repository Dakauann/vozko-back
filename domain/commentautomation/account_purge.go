package commentautomation

import (
	"context"

	"vozko/domain/shared"
)

type AccountPurger interface {
	PurgeAccount(ctx context.Context, source shared.EntryType, accountID string) error
}
