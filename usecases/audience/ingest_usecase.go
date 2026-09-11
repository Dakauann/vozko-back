package audience_usecase

import (
	"context"
	"log"
	"strings"

	"github.com/google/uuid"

	ca "vozko/domain/audience"
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
	metrics   metrics.AudienceMetricsRecorder
	clock     ca.Clock
	// Live tells whoever is looking at the conversation that an analysis is on
	// its way. Optional: without it the queue behaves identically and the state
	// still shows up on the next read, it simply arrives a tick later.
	live ca.ConversationAnalysisLive
}

// SetLive attaches the per-conversation live signal after construction, the way
// the engine's broadcaster is attached: the socket is built long after the
// ingest path and must not become a constructor argument every channel has to
// thread through.
func (uc *ingestUseCase) SetLive(live ca.ConversationAnalysisLive) { uc.live = live }

// NewIngestUseCase builds the Ingestor a channel registers.
func NewIngestUseCase(
	repo ca.Repository,
	settings ca.SettingsResolver,
	scheduler ca.Scheduler,
	rec metrics.AudienceMetricsRecorder,
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
		SubjectID:        in.SubjectID,
		ParentSubjectID:  in.ParentSubjectID,
		AuthorExternalID: in.AuthorExternalID,
		AuthorHandle:     in.AuthorHandle,
		Text:             in.Text,
		OccurredAt:       in.OccurredAt,
		Now:              now,
	})
	if err != nil {
		return err
	}
	row.ID = uuid.NewString()
	if row.Kind() == ca.SubjectKindConversation {
		row.Revision = in.Revision
		row.Transcript = in.Text
		row.MessageCount = in.MessageCount

		// A conversation is queued every time it goes quiet, so this is the one
		// path in the engine that can spend money in a loop. The unique index
		// already refuses an identical transcript; this refuses a snapshot that
		// is new but not new ENOUGH, and refuses to stack a second snapshot
		// behind one that has not been classified yet.
		previous, err := uc.repo.LatestBySubject(ctx, in.WorkspaceID, in.Container.Source, ca.SubjectKindConversation, in.SubjectID)
		if err != nil {
			// Fail closed. Queuing anyway on a failed read is exactly the case
			// the guard exists for, and the next quiet period re-queues.
			return err
		}
		if !row.WorthReanalysing(previous) {
			return nil
		}
	}

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

	// Say so immediately, on the conversation's own channel.
	//
	// The analysis runs minutes later, when the batch does. Without this the
	// only way to learn that anything is happening is to reload, so a reply
	// that quietly queued an analysis looks exactly like a reply that did not.
	if uc.live != nil && row.Kind() == ca.SubjectKindConversation {
		uc.live.AnalysisStateChanged(ca.ConversationAnalysisState{
			EntryID:   row.SubjectID,
			EntryType: string(row.Source),
			Pending:   true,
		})
	}

	// The stamp is best effort by design (plan §2.2): the row is durable, and
	// the backstop finds it without Redis. Failing the webhook over a Redis
	// blink would redeliver a comment that was already stored.
	if err := uc.scheduler.Stamp(ctx, in.Container, strings.TrimSpace(in.WorkspaceID), now); err != nil {
		log.Printf("[comment-analysis] debounce stamp for %s failed (backstop will cover): %v", in.Container.Key(), err)
	}
	return nil
}
