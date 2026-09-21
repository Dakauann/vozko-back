package template

import (
	"context"
	"errors"
	"time"
)

var ErrSendAttemptConflict = errors.New("whatsapp template send attempt: status changed under us")

type SendAttemptRepository interface {
	CreateIfAbsent(ctx context.Context, attempt *SendAttempt) (stored *SendAttempt, created bool, err error)

	FindByID(ctx context.Context, id string) (*SendAttempt, error)
	FindByIdempotencyKey(ctx context.Context, workspaceID, key string) (*SendAttempt, error)
	FindByProviderMessageID(ctx context.Context, workspaceID, providerMessageID string) (*SendAttempt, error)

	MarkCharged(ctx context.Context, id string, chargedMicros int64, at time.Time) error
	MarkSent(ctx context.Context, id string, providerMessageID string, responseStatus int, at time.Time) error
	MarkRejected(ctx context.Context, id string, errorCode int, errorMessage string, responseStatus int) error
	MarkUnknown(ctx context.Context, id string, errorMessage string, responseStatus int) error
	MarkRefunded(ctx context.Context, id string, at time.Time) error

	ListNeedingReconciliation(ctx context.Context, olderThan time.Time, limit int) ([]*SendAttempt, error)
}
