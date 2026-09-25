package scheduled_message_usecase

import (
	"context"

	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

type personScheduler struct {
	access     shared.EntryAccessChecker
	permission templatePermission
	repo       sm.Repository
	schedule   sm.ScheduleUseCase
	reschedule sm.RescheduleUseCase
	cancel     sm.CancelUseCase
}

func NewPersonSchedulerUseCase(
	access shared.EntryAccessChecker,
	permissions workspace.CheckAccessUseCase,
	repo sm.Repository,
	schedule sm.ScheduleUseCase,
	reschedule sm.RescheduleUseCase,
	cancel sm.CancelUseCase,
) sm.PersonSchedulerUseCase {
	return &personScheduler{
		access:     access,
		permission: templatePermission{access: permissions},
		repo:       repo,
		schedule:   schedule,
		reschedule: reschedule,
		cancel:     cancel,
	}
}

func (uc *personScheduler) Schedule(ctx context.Context, by shared.Person, in sm.ScheduleInput) (*sm.ScheduleResult, error) {
	if !by.MayActOn(uc.access, in.WorkspaceID, in.EntryID, in.EntryType) {
		return nil, sm.ErrEntryAccess
	}
	if in.Template != nil {
		if err := uc.permission.require(by, in.WorkspaceID); err != nil {
			return nil, err
		}
	}
	in.CreatedByUserID = by.UserID
	return uc.schedule.Execute(ctx, in)
}

func (uc *personScheduler) Reschedule(ctx context.Context, by shared.Person, in sm.RescheduleInput) (*sm.ScheduleResult, error) {
	if err := uc.authorize(by, in.WorkspaceID, in.ID); err != nil {
		return nil, err
	}
	return uc.reschedule.Execute(ctx, in)
}

func (uc *personScheduler) Cancel(ctx context.Context, by shared.Person, workspaceID, id string) error {
	if err := uc.authorize(by, workspaceID, id); err != nil {
		return err
	}
	return uc.cancel.Execute(ctx, workspaceID, id)
}

func (uc *personScheduler) authorize(by shared.Person, workspaceID, id string) error {
	message, err := loadOwned(uc.repo, workspaceID, id)
	if err != nil {
		return err
	}
	if !by.MayActOn(uc.access, workspaceID, message.EntryID, string(message.EntryType)) {
		return sm.ErrEntryAccess
	}
	if message.Kind == sm.KindTemplate {
		return uc.permission.require(by, workspaceID)
	}
	return nil
}
