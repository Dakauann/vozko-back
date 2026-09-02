package comment_analysis_usecase

import (
	"context"
	"log"
	"strings"

	"github.com/google/uuid"

	ca "vozko/domain/comment_analysis"
	"vozko/domain/metrics"
	"vozko/domain/shared"
)

// Ingest (plan §6.1): one comment becomes one pending row, idempotently,
// plus a debounce stamp. It is what a channel calls from its webhook hot
// path, so it does two cheap things and nothing else.

type ingestUseCase struct {
	repo      ca.Repository
	settings  ca.SettingsResolver
	scheduler ca.Scheduler
	metrics   metrics.CommentAnalysisMetricsRecorder
	clock     ca.Clock
}

// NewIngestUseCase builds the Ingestor a channel registers.
func NewIngestUseCase(
	repo ca.Repository,
	settings ca.SettingsResolver,
	scheduler ca.Scheduler,
	rec metrics.CommentAnalysisMetricsRecorder,
	clock ca.Clock,
) ca.Ingestor {
	if clock == nil {
		clock = shared.SystemClock{}
	}
	return &ingestUseCase{repo: repo, settings: settings, scheduler: scheduler, metrics: rec, clock: clock}
}

func (uc *ingestUseCase) Enqueue(ctx context.Context, in ca.IngestInput) error {
	// Our own replies arrive back as webhooks; analysing our own copy would
	// poison every aggregate. Same guard the rule engine applies.
	if in.IsOurs {
		return nil
	}
	if err := in.Container.Validate(); err != nil {
		return err
	}

	// Off by default: an account nobody configured, or a post whose override
	// switches it off, produces nothing and bills nothing (plan §16).
	settings, err := uc.settings.Resolve(ctx, in.Container)
	if err != nil {
		return err
	}
	if !settings.Enabled {
		return nil
	}

	now := uc.clock.Now()
	row, err := ca.NewPending(ca.NewInput{
		WorkspaceID:      in.WorkspaceID,
		Container:        in.Container,
		SourceCommentID:  in.SourceCommentID,
		ParentCommentID:  in.ParentCommentID,
		AuthorExternalID: in.AuthorExternalID,
		AuthorHandle:     in.AuthorHandle,
		Text:             in.Text,
		CommentedAt:      in.CommentedAt,
		Now:              now,
	})
	if err != nil {
		return err
	}
	row.ID = uuid.NewString()

	inserted, err := uc.repo.Insert(ctx, row)
	if err != nil {
		return err
	}
	if !inserted {
		// A redelivered webhook. The first delivery already stamped the hint.
		return nil
	}
	if uc.metrics != nil {
		uc.metrics.IncCommentEnqueued(string(in.Container.Source))
	}
	if row.Status != ca.StatusPending {
		// Skipped at ingest (blank text): recorded, never scheduled.
		return nil
	}

	// The stamp is best effort by design (plan §2.2): the row is durable, and
	// the backstop finds it without Redis. Failing the webhook over a Redis
	// blink would redeliver a comment that was already stored.
	if err := uc.scheduler.Stamp(ctx, in.Container, strings.TrimSpace(in.WorkspaceID), now); err != nil {
		log.Printf("[comment-analysis] debounce stamp for %s failed (backstop will cover): %v", in.Container.Key(), err)
	}
	return nil
}
