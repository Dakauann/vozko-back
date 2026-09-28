package webhook

import (
	"context"
	"time"
)

type ProcessedEventRepository interface {
	Claim(ctx context.Context, key, channel, accountID string) (claimed bool, err error)
	PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}
