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
	usages, err := uc.caps.ListMonthlySendCapUsage(uc.now())
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
	return &balance.SendCapListing{Items: items, CanUnlock: actor.CanUnlock()}, nil
}

type setMonthlySendCapUseCase struct {
	caps       balance.MonthlySendCapRepository
	workspaces sendCapWorkspaceLookup
	now        func() time.Time
}

func NewSetMonthlySendCapUseCase(caps balance.MonthlySendCapRepository, workspaces sendCapWorkspaceLookup, now func() time.Time) balance.SetMonthlySendCapUseCase {
	return &setMonthlySendCapUseCase{caps: caps, workspaces: workspaces, now: now}
}

func (uc *setMonthlySendCapUseCase) Execute(actor balance.SendCapActor, input balance.SetMonthlySendCapInput) (*balance.MonthlySendCap, error) {
	if !actor.CanManage() {
		return nil, balance.ErrSendCapForbidden
	}
	if _, err := uc.workspaces.GetWorkspaceByID(input.WorkspaceID); err != nil {
		return nil, err
	}
	current, err := uc.caps.GetMonthlySendCap(input.WorkspaceID)
	if err != nil {
		return nil, err
	}
	next, err := uc.candidate(actor, current, input)
	if err != nil {
		return nil, err
	}
	if balance.SendCapChangeRequiresUnlock(current, &next) {
		return nil, balance.ErrSendCapUnlockRequired
	}
	if err := uc.caps.UpsertMonthlySendCap(next); err != nil {
		return nil, err
	}
	log.Printf("[monthly-send-cap] workspace %s capped at %d from day %d by %s", input.WorkspaceID, next.Limit, next.CycleDay, actor.UserID)
	return &next, nil
}

func (uc *setMonthlySendCapUseCase) candidate(actor balance.SendCapActor, current *balance.MonthlySendCap, input balance.SetMonthlySendCapInput) (balance.MonthlySendCap, error) {
	if current == nil {
		cycleDay := 1
		if input.CycleDay != nil {
			cycleDay = *input.CycleDay
		}
		return balance.NewMonthlySendCap(input.WorkspaceID, input.Limit, cycleDay, actor.UserID, uc.now())
	}
	relimited, err := current.Relimited(input.Limit, actor.UserID, uc.now())
	if err != nil {
		return balance.MonthlySendCap{}, err
	}
	return relimited.RecycledIfAsked(input.CycleDay)
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
	if err := balance.VerifySendCapUnlockCode(actor.Email, input.Code); err != nil {
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
	if unlocked, err = unlocked.RecycledIfAsked(input.CycleDay); err != nil {
		return nil, err
	}
	if err := uc.caps.UpsertMonthlySendCap(unlocked); err != nil {
		return nil, err
	}
	log.Printf("[monthly-send-cap] workspace %s cap changed from %d (day %d) to %d (day %d) by %s", input.WorkspaceID, current.Limit, current.CycleDay, unlocked.Limit, unlocked.CycleDay, actor.UserID)
	return &unlocked, nil
}
