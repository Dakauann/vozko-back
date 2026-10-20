package commentautomation

import (
	"context"
	"errors"
	"time"

	"vozko/domain/shared"
)

const ExecutionRetention = 30 * 24 * time.Hour

var (
	ErrRulesUnavailable   = errors.New("comment rules are unavailable")
	ErrExecutionUnclaimed = errors.New("comment rule execution could not be claimed")
	ErrActionFailed       = errors.New("comment rule action failed")
	ErrCommentIDRequired  = errors.New("comment rules need the comment id")
)

type ExecutionClaims interface {
	Claim(ctx context.Context, source shared.EntryType, commentID, ruleID string) (bool, error)
	PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

func Retryable(err error) bool {
	return errors.Is(err, ErrRulesUnavailable) || errors.Is(err, ErrExecutionUnclaimed)
}
