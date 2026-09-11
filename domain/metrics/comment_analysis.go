package metrics

import "time"

// Comment-analysis label values (plan §14).
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

// AudienceMetricsRecorder is what the comment-analysis engine
// reports. comment_analysis_pending rising monotonically is the alert that
// matters: it means the flush is not keeping up with ingest, which is the
// only way the feature fails quietly.
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
