package inbox_assignment_usecase

import (
	"fmt"
	"strings"

	"vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/shared"
)

type AssignTargets interface {
	GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool)
}

type MemberReach interface {
	CanView(callerUserID, targetUserID, workspaceID string, isPlatformAdmin bool) (bool, error)
}

type EntryPhones interface {
	BusinessPhoneForEntry(entryID, entryType string) string
}

type ManualAssigner interface {
	AssignManual(entryID, entryType, businessPhoneID, workspaceID, toUserID, assignedBy, trigger string) error
}

type RouletteDealer interface {
	HandOffToRoulette(in ia.RouletteHandOff) (string, error)
}

type PersonAssignDeps struct {
	Access     shared.EntryAccessChecker
	Targets    AssignTargets
	Visibility MemberReach
	Phones     EntryPhones
	Assigner   ManualAssigner
	Roulette   RouletteDealer
}

type personAssign struct{ deps PersonAssignDeps }

func NewPersonAssignUseCase(deps PersonAssignDeps) ia.PersonAssignUseCase {
	return &personAssign{deps: deps}
}

func (uc *personAssign) Assign(by shared.Person, workspaceID, entryID, entryType, toUserID string) error {
	toUserID = strings.TrimSpace(toUserID)
	if toUserID == "" || !by.MayActOn(uc.deps.Access, workspaceID, entryID, entryType) {
		return ia.ErrAssignEntryAccess
	}
	if uc.deps.Targets == nil {
		return ia.ErrAssignTargetIneligible
	}
	if _, eligible := uc.deps.Targets.GetDepartmentScope(toUserID, workspaceID, false); !eligible {
		return ia.ErrAssignTargetIneligible
	}
	if uc.deps.Visibility == nil {
		return ia.ErrAssignTargetOutOfReach
	}
	reachable, err := uc.deps.Visibility.CanView(by.UserID, toUserID, workspaceID, by.SystemAdmin)
	if err != nil {
		return fmt.Errorf("check reach of %s: %w", toUserID, err)
	}
	if !reachable {
		return ia.ErrAssignTargetOutOfReach
	}
	phone := ""
	if uc.deps.Phones != nil {
		phone = uc.deps.Phones.BusinessPhoneForEntry(entryID, entryType)
	}
	return uc.deps.Assigner.AssignManual(entryID, entryType, phone, workspaceID, toUserID, by.UserID, ia.TriggerManual)
}

func (uc *personAssign) HandOff(by shared.Person, workspaceID, entryID, entryType, departmentID string) (string, error) {
	departmentID = strings.TrimSpace(departmentID)
	if departmentID == "" || !by.MayActOn(uc.deps.Access, workspaceID, entryID, entryType) {
		return "", ia.ErrAssignEntryAccess
	}
	if uc.deps.Roulette == nil {
		return "", ia.ErrDepartmentOutOfScope
	}
	return uc.deps.Roulette.HandOffToRoulette(ia.RouletteHandOff{
		WorkspaceID:  workspaceID,
		EntryID:      entryID,
		EntryType:    entryType,
		DepartmentID: departmentID,
		ByActorID:    by.UserID,
	})
}
