package message_shortcut_usecase

import "vozko/domain/message_shortcut"

type deleteUseCase struct {
	repo message_shortcut.Repository
}

func NewDeleteUseCase(repo message_shortcut.Repository) message_shortcut.DeleteUseCase {
	return &deleteUseCase{repo: repo}
}

func (uc *deleteUseCase) Execute(workspaceID, id string) error {
	existing, err := uc.repo.FindByID(id)
	if err != nil {
		return err
	}
	if existing.WorkspaceID != workspaceID {
		return message_shortcut.ErrNotFound
	}
	return uc.repo.Delete(id)
}
