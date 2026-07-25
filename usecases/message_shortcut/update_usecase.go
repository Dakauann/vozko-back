package message_shortcut_usecase

import (
	"strings"

	"vozko/domain/message_shortcut"
)

type updateUseCase struct {
	repo message_shortcut.Repository
}

func NewUpdateUseCase(repo message_shortcut.Repository) message_shortcut.UpdateUseCase {
	return &updateUseCase{repo: repo}
}

func (uc *updateUseCase) Execute(workspaceID, id string, input message_shortcut.UpdateInput) (*message_shortcut.MessageShortcut, error) {
	existing, err := uc.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if existing.WorkspaceID != workspaceID {
		return nil, message_shortcut.ErrNotFound
	}

	if input.Name != nil {
		existing.Name = strings.TrimSpace(*input.Name)
	}
	if input.Shortcut != nil {
		existing.Shortcut = strings.ToLower(strings.TrimSpace(*input.Shortcut))
	}
	if input.MessageType != nil {
		existing.MessageType = *input.MessageType
	}
	if input.Content != nil {
		existing.Content = *input.Content
	}

	if err := existing.Validate(); err != nil {
		return nil, err
	}

	if input.Shortcut != nil {
		exists, err := uc.repo.ShortcutExistsInWorkspace(workspaceID, existing.Shortcut, &id)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, message_shortcut.ErrShortcutExists
		}
	}

	if err := uc.repo.Update(existing); err != nil {
		return nil, err
	}
	return existing, nil
}
