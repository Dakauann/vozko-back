package template

import (
	"context"
	"errors"
	"time"
)

// ErrSendAttemptConflict is a guarded update that matched nothing: the row had
// already moved on. It is a normal outcome under concurrency, not a failure —
// it means somebody else won the race and the caller should stop.
var ErrSendAttemptConflict = errors.New("whatsapp template send attempt: status changed under us")

// SendAttemptRepository is the port the exactly-once guarantee rests on.
//
// Every mutating method is expressed as a compare-and-set rather than a read
// followed by a write, because the caller is one of several replicas racing on
// the same row and a read-then-write between them is a double charge.
type SendAttemptRepository interface {
	// CreateIfAbsent inserts the attempt, or returns the one already stored under
	// the same (workspace, idempotency key).
	//
	// created distinguishes the winner from everyone else. Exactly one caller
	// across all replicas may see true, and only that caller is allowed to spend
	// money — which is why this returns a bool rather than swallowing the
	// conflict and returning the row.
	CreateIfAbsent(ctx context.Context, attempt *SendAttempt) (stored *SendAttempt, created bool, err error)

	FindByID(ctx context.Context, id string) (*SendAttempt, error)
	FindByIdempotencyKey(ctx context.Context, workspaceID, key string) (*SendAttempt, error)
	// FindByProviderMessageID resolves the attempt a delivery-status webhook is
	// talking about when the webhook carries no correlation id of ours.
	FindByProviderMessageID(ctx context.Context, workspaceID, providerMessageID string) (*SendAttempt, error)

	// MarkCharged is the money gate. It must be the last thing that happens
	// before the provider call and the first thing that fails if somebody else
	// already crossed it.
	MarkCharged(ctx context.Context, id string, chargedMicros int64, at time.Time) error
	MarkSent(ctx context.Context, id string, providerMessageID string, responseStatus int, at time.Time) error
	MarkRejected(ctx context.Context, id string, errorCode int, errorMessage string, responseStatus int) error
	MarkUnknown(ctx context.Context, id string, errorMessage string, responseStatus int) error
	MarkRefunded(ctx context.Context, id string, at time.Time) error

	// ListNeedingReconciliation returns attempts that took money and never
	// reached a terminal state, oldest first.
	ListNeedingReconciliation(ctx context.Context, olderThan time.Time, limit int) ([]*SendAttempt, error)
}
