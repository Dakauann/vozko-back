package message_shortcut_usecase

import "vozko/domain/message_shortcut"

type listUseCase struct {
	repo message_shortcut.Repository
}

func NewListUseCase(repo message_shortcut.Repository) message_shortcut.ListUseCase {
	return &listUseCase{repo: repo}
}

func (uc *listUseCase) Execute(workspaceID string) ([]*message_shortcut.MessageShortcut, error) {
	return uc.repo.ListByWorkspace(workspaceID)
}
