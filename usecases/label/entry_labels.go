package label_usecase

import (
	"vozko/domain/label"
	"vozko/domain/shared"
)

type entryLabels struct {
	access shared.EntryAccessChecker
	apply  label.AssignEntryLabelUseCase
	remove label.RemoveEntryLabelUseCase
}

func NewEntryLabelsUseCase(access shared.EntryAccessChecker, apply label.AssignEntryLabelUseCase, remove label.RemoveEntryLabelUseCase) label.EntryLabelsUseCase {
	return &entryLabels{access: access, apply: apply, remove: remove}
}

func (uc *entryLabels) Apply(workspaceID string, by shared.Person, input label.AssignEntryLabelInput) (*label.EntryLabel, error) {
	if !by.MayActOn(uc.access, workspaceID, input.EntryID, input.EntryType) {
		return nil, label.ErrEntryAccess
	}
	input.ActorID = by.UserID
	return uc.apply.Execute(workspaceID, input)
}

func (uc *entryLabels) Remove(workspaceID string, by shared.Person, input label.RemoveEntryLabelInput) error {
	if !by.MayActOn(uc.access, workspaceID, input.EntryID, input.EntryType) {
		return label.ErrEntryAccess
	}
	input.ActorID = by.UserID
	return uc.remove.Execute(workspaceID, input)
}
