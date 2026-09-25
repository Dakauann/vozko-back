package scheduled_message

import (
	"context"
	"errors"

	"vozko/domain/shared"
)

var ErrEntryAccess = errors.New("scheduled message: no access to this conversation")

type PersonSchedulerUseCase interface {
	Schedule(ctx context.Context, by shared.Person, in ScheduleInput) (*ScheduleResult, error)
	Reschedule(ctx context.Context, by shared.Person, in RescheduleInput) (*ScheduleResult, error)
	Cancel(ctx context.Context, by shared.Person, workspaceID, id string) error
}
