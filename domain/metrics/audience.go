package metrics

import "time"

const (
	CommentBatchOutcomeOK            = "ok"
	CommentBatchOutcomeLength        = "length"
	CommentBatchOutcomeParseError    = "parse_error"
	CommentBatchOutcomeProviderError = "provider_error"

	CommentItemOutcomeAnalyzed   = "analyzed"
	CommentItemOutcomeMissingRef = "missing_ref"
	CommentItemOutcomeInvalidRef = "invalid_ref"
	CommentItemOutcomeBadLabels  = "bad_labels"
	CommentItemOutcomeFailed     = "failed"
	CommentItemOutcomeSkipped    = "skipped"
	CommentItemOutcomeDeferred   = "deferred"

	CommentTokenKindPrompt     = "prompt"
	CommentTokenKindCompletion = "completion"

	CommentCapCycle   = "cycle"
	CommentCapDaily   = "daily"
	CommentCapBalance = "balance"
)

type AudienceMetricsRecorder interface {
	IncCommentEnqueued(source string)
	IncCommentBatches(model, outcome string)
	AddCommentItems(outcome string, n int)
	AddCommentTokens(model, kind string, n int)
	ObserveCommentBatchLatency(elapsed time.Duration)
	SetCommentPending(source string, n int)
	AddCommentTruncated(n int)
	IncCommentCapHit(cap string)
}
