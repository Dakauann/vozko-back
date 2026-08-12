package scheduled_message_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"

	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
)

type scheduleUseCase struct {
	repo    sm.Repository
	windows *windowService
	wake    sm.WakeScheduler
	clock   sm.Clock
}

// NewScheduleUseCase wires the create path.
//
// Every dependency is required. The wake scheduler in particular looks optional
// — the sweep would still deliver everything — but accepting a nil one here
// would silently degrade every schedule to up-to-a-minute-late, which is a
// product regression nobody would notice until a customer did.
func NewScheduleUseCase(
	repo sm.Repository,
	windows sm.WindowReader,
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
	if wake == nil {
		missing = append(missing, "wake scheduler")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("scheduled message schedule use case: missing %s", strings.Join(missing, ", "))
	}

	return &scheduleUseCase{repo: repo, windows: windowSvc, wake: wake, clock: clock}, nil
}

func (uc *scheduleUseCase) Execute(_ context.Context, in sm.ScheduleInput) (*sm.ScheduleResult, error) {
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
		ScheduledAt:      in.ScheduledAt.UTC(),
		IdempotencyKey:   optional(in.IdempotencyKey),
		Status:           sm.StatusPending,
	}
	message.Normalize()
	if err := message.Validate(); err != nil {
		return nil, err
	}

	window, err := uc.windows.Validate(message.EntryID, string(message.EntryType), message.ScheduledAt)
	if err != nil {
		// The window travels back with the refusal so the caller can tell the
		// operator which boundary they hit rather than just that they missed.
		return &sm.ScheduleResult{Window: window}, err
	}
	message.WindowExpiresAtAtCreation = window.ExpiresAt

	if err := uc.repo.Create(message); err != nil {
		return nil, err
	}

	uc.enqueue(message)
	return &sm.ScheduleResult{Message: message, Window: window}, nil
}

// replay returns the message an identical earlier request created.
//
// This is what makes a retried POST — a double-click, a timeout the client
// retried, a proxy replaying a request — produce one scheduled message instead
// of two identical ones arriving at the customer a moment apart.
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

// enqueue asks for a timely delivery. Best-effort by design: the row is already
// durable and pending, so a broker that is down costs latency (the sweep picks
// it up within a minute) and never the message.
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
