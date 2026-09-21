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

type ingestUseCase struct {
	repo      ca.Repository
	settings  ca.SettingsResolver
	scheduler ca.Scheduler
	metrics   metrics.AudienceMetricsRecorder
	clock     ca.Clock
	live      ca.ConversationAnalysisLive
}

func (uc *ingestUseCase) SetLive(live ca.ConversationAnalysisLive) { uc.live = live }

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
	if in.IsOurs {
		return nil
	}
	if err := in.Container.Validate(); err != nil {
		return err
	}

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

		previous, err := uc.repo.LatestBySubject(ctx, in.WorkspaceID, in.Container.Source, ca.SubjectKindConversation, in.SubjectID)
		if err != nil {
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
		return nil
	}
	if uc.metrics != nil {
		uc.metrics.IncCommentEnqueued(string(in.Container.Source))
	}
	if row.Status != ca.StatusPending {
		return nil
	}

	if uc.live != nil && row.Kind() == ca.SubjectKindConversation {
		uc.live.AnalysisStateChanged(ca.ConversationAnalysisState{
			EntryID:   row.SubjectID,
			EntryType: string(row.Source),
			Pending:   true,
		})
	}

	if err := uc.scheduler.Stamp(ctx, in.Container, strings.TrimSpace(in.WorkspaceID), now); err != nil {
		log.Printf("[comment-analysis] debounce stamp for %s failed (backstop will cover): %v", in.Container.Key(), err)
	}
	return nil
}
