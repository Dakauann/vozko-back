package comment_analysis

import (
	"context"
	"time"

	"vozko/domain/shared"
)

// Narrow outbound ports. Each is the smallest interface the engine needs
// from something outside it, so a channel, a provider or Redis can be swapped
// or faked without the engine noticing. It is the posture CommentRuleEvaluator
// and AnalysisSubjectResolver already take.

// ---- Inbound: what a channel calls ----

// IngestInput is what a channel knows about one comment.
type IngestInput struct {
	WorkspaceID      string
	Container        ContainerRef
	SourceCommentID  string
	ParentCommentID  string
	AuthorExternalID string
	AuthorHandle     string
	Text             string
	// CommentedAt is the channel's timestamp; zero means unknown.
	CommentedAt time.Time
	// IsOurs marks the account's own comment. Our replies arrive back as
	// webhooks, and analysing our own copy would poison every aggregate.
	IsOurs bool
}

// Ingestor is what a channel calls for each mirrored comment. Best effort:
// the channel logs the error and moves on, because failing an inbound
// webhook over a queue hiccup would redeliver a message that was already
// stored.
type Ingestor interface {
	Enqueue(ctx context.Context, in IngestInput) error
}

// ---- Outbound: what the engine asks of a channel ----

// ContainerContext is what the prompt says about the post: the model needs
// the caption to tell a critic from a supporter.
type ContainerContext struct {
	Caption     string
	Permalink   string
	PublishedAt *time.Time
}

// SourceAdapter is a channel's side of the engine, registered per Source.
// The engine never stores comment bodies (§5.1); it reads them back from the
// channel's own table at classification time.
type SourceAdapter interface {
	// ReadTexts returns the text of each comment by source id. Missing ids
	// (deleted since ingest) are simply absent from the map.
	ReadTexts(ctx context.Context, ref ContainerRef, sourceCommentIDs []string) (map[string]string, error)
	ReadContainerContext(ctx context.Context, ref ContainerRef) (ContainerContext, error)
	// ListContainers enumerates an account's posts for a backfill, from the
	// channel's local projection (no provider calls).
	ListContainers(ctx context.Context, accountID string, limit, offset int) ([]ContainerSummary, error)
	// FetchCommentsPage pulls one page of a container's comments off the
	// provider for a backfill. The returned cursor is opaque; empty means
	// the edge is exhausted.
	FetchCommentsPage(ctx context.Context, ref ContainerRef, cursor string) (items []IngestInput, nextCursor string, err error)
}

// ContainerSummary is one post as the backfill sees it.
type ContainerSummary struct {
	Ref           ContainerRef
	WorkspaceID   string
	CommentsCount int
}

// ---- Classification ----

// ClassifyRequest is one batch to send.
type ClassifyRequest struct {
	WorkspaceID string
	Model       string
	Topics      TopicSet
	Context     ContainerContext
	// Instructions is the operator's context (account and post), already
	// resolved; empty when none was written.
	Instructions string
	Batch        BatchPlan
}

// ClassifyResult is what came back, raw. The use case reconciles refs and
// validates labels; the classifier only transports.
type ClassifyResult struct {
	Results          []BatchResult
	FinishReason     string
	Model            string
	RequestID        string
	PromptTokens     int
	CompletionTokens int
}

// Classifier runs one batch against the model. Implemented over ai.Service;
// a fake stands in for it in every engine test.
type Classifier interface {
	Classify(ctx context.Context, req ClassifyRequest) (*ClassifyResult, error)
}

// ---- Debounce hints ----

// Scheduler keeps the debounce hints (§6.2). Losing all of them costs one
// backstop interval of latency and never a comment.
type Scheduler interface {
	Stamp(ctx context.Context, ref ContainerRef, workspaceID string, now time.Time) error
	Hints(ctx context.Context) ([]Hint, error)
	Clear(ctx context.Context, ref ContainerRef) error
}

// ---- Billing ----

// Charger is the engine's side of billing (§9). Token billing needs none
// of this: setting WorkspaceID on the AI call is the whole integration.
// This covers the optional per-comment surcharge and the daily cap.
type Charger interface {
	// ReserveDaily claims items against the workspace's cap for the day and
	// reports false when the claim would exceed it.
	ReserveDaily(ctx context.Context, workspaceID string, items, cap int, day time.Time) (bool, error)
	// ChargeBatch debits the per-batch surcharge, idempotent on batchID.
	// A configured price of 0 is "token billing only", not an error.
	ChargeBatch(ctx context.Context, workspaceID, batchID string, items int) (priceMicros int64, err error)
}

// Clock is the source of "now" (domain/shared/clock.go), injected so the
// debounce and lease arithmetic is testable without sleeping.
type Clock = shared.Clock

// ---- Settings resolution ----

// SettingsResolver answers "what settings apply to this post": the account's
// with the post's override on top, or the disabled defaults for an account
// nobody configured. It never returns ErrNotFound; an unknown account simply
// resolves to "off". The engine and the ingest path depend on this, not on
// the repository, so the fallback rule lives in exactly one place.
type SettingsResolver interface {
	Resolve(ctx context.Context, ref ContainerRef) (*Settings, error)
}
