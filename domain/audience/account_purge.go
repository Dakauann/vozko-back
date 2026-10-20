package audience

import "context"

type AccountPurger interface {
	PurgeAccount(ctx context.Context, source Source, accountID string) error
}
