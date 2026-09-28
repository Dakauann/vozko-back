package balance_usecase

import (
	"log"
	"time"

	"vozko/domain/balance"
	"vozko/domain/workspace"
)

type sendCapWorkspaceLookup interface {
	GetWorkspaceByID(id string) (*workspace.Workspace, error)
}

type listMonthlySendCapsUseCase struct {
	caps balance.MonthlySendCapRepository
	now  func() time.Time
}

func NewListMonthlySendCapsUseCase(caps balance.MonthlySendCapRepository, now func() time.Time) balance.ListMonthlySendCapsUseCase {
	return &listMonthlySendCapsUseCase{caps: caps, now: now}
}

func (uc *listMonthlySendCapsUseCase) Execute(actor balance.SendCapActor, level balance.SendCapLevel) (*balance.SendCapListing, error) {
	if !actor.CanManage() {
		return nil, balance.ErrSendCapForbidden
	}
	monthStart := balance.SendCapMonthStart(uc.now())
	usages, err := uc.caps.ListMonthlySendCapUsage(monthStart)
	if err != nil {
		return nil, err
	}
	items := make([]balance.SendCapUsage, 0, len(usages))
	for _, usage := range usages {
		if usage.Matches(level) {
			items = append(items, usage)
		}
	}
	balance.SortSendCapUsageByPressure(items)
	return &balance.SendCapListing{MonthStart: monthStart, Items: items, CanUnlock: actor.CanUnlock()}, nil
}

type setMonthlySendCapUseCase struct {
	caps       balance.MonthlySendCapRepository
	workspaces sendCapWorkspaceLookup
	now        func() time.Time
}

func NewSetMonthlySendCapUseCase(caps balance.MonthlySendCapRepository, workspaces sendCapWorkspaceLookup, now func() time.Time) balance.SetMonthlySendCapUseCase {
	return &setMonthlySendCapUseCase{caps: caps, workspaces: workspaces, now: now}
}

func (uc *setMonthlySendCapUseCase) Execute(actor balance.SendCapActor, workspaceID string, limit int64) (*balance.MonthlySendCap, error) {
	if !actor.CanManage() {
		return nil, balance.ErrSendCapForbidden
	}
	if _, err := uc.workspaces.GetWorkspaceByID(workspaceID); err != nil {
		return nil, err
	}
	current, err := uc.caps.GetMonthlySendCap(workspaceID)
	if err != nil {
		return nil, err
	}
	if balance.SendCapChangeRequiresUnlock(current, &limit) {
		return nil, balance.ErrSendCapUnlockRequired
	}

	var next balance.MonthlySendCap
	if current == nil {
		next, err = balance.NewMonthlySendCap(workspaceID, limit, actor.UserID, uc.now())
	} else {
		next, err = current.Relimited(limit, actor.UserID, uc.now())
	}
	if err != nil {
		return nil, err
	}
	if err := uc.caps.UpsertMonthlySendCap(next); err != nil {
		return nil, err
	}
	log.Printf("[monthly-send-cap] workspace %s capped at %d by %s", workspaceID, limit, actor.UserID)
	return &next, nil
}

type unlockMonthlySendCapUseCase struct {
	caps balance.MonthlySendCapRepository
	now  func() time.Time
}

func NewUnlockMonthlySendCapUseCase(caps balance.MonthlySendCapRepository, now func() time.Time) balance.UnlockMonthlySendCapUseCase {
	return &unlockMonthlySendCapUseCase{caps: caps, now: now}
}

func (uc *unlockMonthlySendCapUseCase) Execute(actor balance.SendCapActor, input balance.UnlockMonthlySendCapInput) (*balance.MonthlySendCap, error) {
	if !actor.CanUnlock() {
		return nil, balance.ErrSendCapForbidden
	}
	if err := balance.VerifySendCapUnlockCode(input.Code); err != nil {
		log.Printf("[monthly-send-cap] rejected unlock code for workspace %s from %s", input.WorkspaceID, actor.UserID)
		return nil, err
	}
	current, err := uc.caps.GetMonthlySendCap(input.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, balance.ErrMonthlySendCapNotFound
	}

	if input.Limit == nil {
		if err := uc.caps.DeleteMonthlySendCap(input.WorkspaceID); err != nil {
			return nil, err
		}
		log.Printf("[monthly-send-cap] workspace %s cap removed by %s", input.WorkspaceID, actor.UserID)
		return nil, nil
	}

	unlocked, err := current.Unlocked(*input.Limit, actor.UserID, uc.now())
	if err != nil {
		return nil, err
	}
	if err := uc.caps.UpsertMonthlySendCap(unlocked); err != nil {
		return nil, err
	}
	log.Printf("[monthly-send-cap] workspace %s cap changed from %d to %d by %s", input.WorkspaceID, current.Limit, unlocked.Limit, actor.UserID)
	return &unlocked, nil
}
