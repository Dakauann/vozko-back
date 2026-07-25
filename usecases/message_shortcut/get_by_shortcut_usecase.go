package message_shortcut_usecase

import "vozko/domain/message_shortcut"

type getByShortcutUseCase struct {
	repo message_shortcut.Repository
}

func NewGetByShortcutUseCase(repo message_shortcut.Repository) message_shortcut.GetByShortcutUseCase {
	return &getByShortcutUseCase{repo: repo}
}

func (uc *getByShortcutUseCase) Execute(workspaceID, shortcut string) (*message_shortcut.MessageShortcut, error) {
	return uc.repo.FindByShortcut(workspaceID, shortcut)
}
