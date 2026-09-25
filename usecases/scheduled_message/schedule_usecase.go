package scheduled_message_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"

	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
	wo "vozko/domain/whatsapp_outreach"
)

type scheduleUseCase struct {
	repo      sm.Repository
	windows   *windowService
	templates wo.ConversationTemplateUseCase
	wake      sm.WakeScheduler
	clock     sm.Clock
}

func NewScheduleUseCase(
	repo sm.Repository,
	windows sm.WindowReader,
	templates wo.ConversationTemplateUseCase,
	wake sm.WakeScheduler,
	clock sm.Clock,
) (sm.ScheduleUseCase, error) {
	windowSvc, err := newWindowService(windows, clock)
	if err != nil {
		return nil, err
	}
	missing := []string{}
	if repo == nil {
		missing = append(missing, "repository")
	}
	if templates == nil {
		missing = append(missing, "template check")
	}
	if wake == nil {
		missing = append(missing, "wake scheduler")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("scheduled message schedule use case: missing %s", strings.Join(missing, ", "))
	}

	return &scheduleUseCase{repo: repo, windows: windowSvc, templates: templates, wake: wake, clock: clock}, nil
}

func (uc *scheduleUseCase) Execute(ctx context.Context, in sm.ScheduleInput) (*sm.ScheduleResult, error) {
	if existing, err := uc.replay(in); existing != nil || err != nil {
		return existing, err
	}

	message := &sm.ScheduledMessage{
		ID:               uuid.NewString(),
		WorkspaceID:      in.WorkspaceID,
		EntryID:          in.EntryID,
		EntryType:        shared.EntryType(in.EntryType),
		CreatedByUserID:  in.CreatedByUserID,
		Text:             in.Text,
		MediaID:          optional(in.MediaID),
		MediaType:        optional(in.MediaType),
		ReplyToMessageID: optional(in.ReplyToMessageID),
		Signed:           in.Signed,
		Kind:             kindOf(in),
		Template:         templateOf(in),
		ScheduledAt:      in.ScheduledAt.UTC(),
		IdempotencyKey:   optional(in.IdempotencyKey),
		Status:           sm.StatusPending,
	}
	message.Normalize()
	if err := message.Validate(); err != nil {
		return nil, err
	}

	window, err := uc.windows.Validate(message.EntryID, string(message.EntryType), message.Kind, message.ScheduledAt)
	if err != nil {
		return &sm.ScheduleResult{Window: window}, err
	}
	message.WindowExpiresAtAtCreation = window.ExpiresAt

	if message.Kind == sm.KindTemplate {
		if err := uc.checkTemplate(ctx, message); err != nil {
			return &sm.ScheduleResult{Window: window}, err
		}
	}

	if err := uc.repo.Create(message); err != nil {
		return nil, err
	}

	uc.enqueue(message)
	return &sm.ScheduleResult{Message: message, Window: window}, nil
}

func (uc *scheduleUseCase) checkTemplate(ctx context.Context, m *sm.ScheduledMessage) error {
	checked, err := uc.templates.Check(ctx, templateSend(m))
	if err != nil {
		return err
	}
	m.Template.Name = checked.Name
	m.Template.Preview = checked.Preview
	return nil
}

func kindOf(in sm.ScheduleInput) sm.Kind {
	if in.Template != nil {
		return sm.KindTemplate
	}
	return sm.KindText
}

func templateOf(in sm.ScheduleInput) *sm.TemplateContent {
	if in.Template == nil {
		return nil
	}
	return &sm.TemplateContent{
		ID:           in.Template.ID,
		BodyParams:   in.Template.BodyParams,
		HeaderParams: in.Template.HeaderParams,
	}
}

func (uc *scheduleUseCase) replay(in sm.ScheduleInput) (*sm.ScheduleResult, error) {
	key := strings.TrimSpace(in.IdempotencyKey)
	if key == "" {
		return nil, nil
	}

	existing, err := uc.repo.FindByIdempotencyKey(in.WorkspaceID, key)
	if err != nil {
		if err == sm.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}

	return &sm.ScheduleResult{
		Message:        existing,
		Window:         uc.windows.State(existing.EntryID, string(existing.EntryType)),
		AlreadyExisted: true,
	}, nil
}

func (uc *scheduleUseCase) enqueue(m *sm.ScheduledMessage) {
	if err := uc.wake.ScheduleFire(m.ID, m.ScheduledAt); err != nil {
		log.Printf("[scheduled_message] could not enqueue %s for %s: %v; the sweep will deliver it",
			m.ID, m.ScheduledAt.Format("2006-01-02T15:04:05Z"), err)
	}
}

func optional(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

var _ sm.ScheduleUseCase = (*scheduleUseCase)(nil)
