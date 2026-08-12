package scheduled_message_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"

	sm "vozko/domain/scheduled_message"
)

// cancelUseCase and rescheduleUseCase share a fixture because they share a
// precondition: both only ever act on a message that is still pending, and both
// let the database decide the outcome when they race a dispatch.

type cancelUseCase struct {
	repo sm.Repository
}

func NewCancelUseCase(repo sm.Repository) (sm.CancelUseCase, error) {
	if repo == nil {
		return nil, fmt.Errorf("scheduled message cancel use case: missing repository")
	}
	return &cancelUseCase{repo: repo}, nil
}

// Execute cancels a pending message.
//
// The workspace check is an authorization boundary, not a filter: without it a
// member of one tenant could cancel another tenant's message by id.
//
// Cancelling something already sent returns ErrNotPending rather than
// succeeding quietly. The operator needs to know the customer has it.
func (uc *cancelUseCase) Execute(_ context.Context, workspaceID, id string) error {
	message, err := uc.load(workspaceID, id)
	if err != nil {
		return err
	}
	return uc.repo.Cancel(message.ID)
}

func (uc *cancelUseCase) load(workspaceID, id string) (*sm.ScheduledMessage, error) {
	return loadOwned(uc.repo, workspaceID, id)
}

type rescheduleUseCase struct {
	repo    sm.Repository
	windows *windowService
	wake    sm.WakeScheduler
}

func NewRescheduleUseCase(
	repo sm.Repository,
	windows sm.WindowReader,
	wake sm.WakeScheduler,
	clock sm.Clock,
) (sm.RescheduleUseCase, error) {
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
		return nil, fmt.Errorf("scheduled message reschedule use case: missing %s", strings.Join(missing, ", "))
	}
	return &rescheduleUseCase{repo: repo, windows: windowSvc, wake: wake}, nil
}

// Execute moves a pending message to a new time.
//
// The new time is validated against the window as it is NOW, not as it was when
// the message was created. The window can only have grown, so this is never
// stricter than the original check — but it is the honest one, and it is what
// lets an operator push a message further out after the customer writes again.
func (uc *rescheduleUseCase) Execute(_ context.Context, in sm.RescheduleInput) (*sm.ScheduleResult, error) {
	message, err := loadOwned(uc.repo, in.WorkspaceID, in.ID)
	if err != nil {
		return nil, err
	}
	if message.Status != sm.StatusPending {
		return nil, sm.ErrNotPending
	}

	at := in.ScheduledAt.UTC()
	window, err := uc.windows.Validate(message.EntryID, string(message.EntryType), at)
	if err != nil {
		return &sm.ScheduleResult{Window: window}, err
	}

	if err := uc.repo.Reschedule(message.ID, at, window.ExpiresAt); err != nil {
		return nil, err
	}
	message.ScheduledAt = at
	message.WindowExpiresAtAtCreation = window.ExpiresAt

	// A second fire signal for the same id is harmless: the claim admits one
	// caller, so the stale signal from the original time simply finds the row
	// already gone or not yet due.
	if err := uc.wake.ScheduleFire(message.ID, at); err != nil {
		log.Printf("[scheduled_message] could not re-enqueue %s: %v; the sweep will deliver it", message.ID, err)
	}

	return &sm.ScheduleResult{Message: message, Window: window}, nil
}

type listUseCase struct {
	repo    sm.Repository
	windows *windowService
}

func NewListUseCase(repo sm.Repository, windows sm.WindowReader, clock sm.Clock) (sm.ListUseCase, error) {
	windowSvc, err := newWindowService(windows, clock)
	if err != nil {
		return nil, err
	}
	if repo == nil {
		return nil, fmt.Errorf("scheduled message list use case: missing repository")
	}
	return &listUseCase{repo: repo, windows: windowSvc}, nil
}

// ForEntry returns the conversation's scheduled messages together with its live
// window.
//
// The window rides along because the composer needs it to decide whether to
// offer scheduling at all, and asking for it separately would mean two requests
// that can disagree by the width of the round trip.
func (uc *listUseCase) ForEntry(_ context.Context, entryID, entryType string, statuses []sm.Status) (*sm.ListForEntryResult, error) {
	messages, err := uc.repo.ListByEntry(entryID, entryType, statuses)
	if err != nil {
		return nil, err
	}
	return &sm.ListForEntryResult{
		Messages: messages,
		Window:   uc.windows.State(entryID, entryType),
	}, nil
}

func (uc *listUseCase) ForWorkspace(_ context.Context, workspaceID string, q sm.ListQuery) ([]*sm.ScheduledMessage, int64, error) {
	return uc.repo.ListByWorkspace(workspaceID, q)
}

// loadOwned reads a message and refuses it if it belongs to another workspace.
//
// ErrNotFound rather than a distinct "forbidden": telling a caller that an id
// they cannot see exists is itself a leak.
func loadOwned(repo sm.Repository, workspaceID, id string) (*sm.ScheduledMessage, error) {
	message, err := repo.FindByID(strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if message.WorkspaceID != workspaceID {
		return nil, sm.ErrNotFound
	}
	return message, nil
}

var (
	_ sm.CancelUseCase     = (*cancelUseCase)(nil)
	_ sm.RescheduleUseCase = (*rescheduleUseCase)(nil)
	_ sm.ListUseCase       = (*listUseCase)(nil)
)
