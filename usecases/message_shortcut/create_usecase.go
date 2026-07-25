package message_shortcut_usecase

import (
	"strings"

	"github.com/google/uuid"

	"vozko/domain/message_shortcut"
)

type createUseCase struct {
	repo message_shortcut.Repository
}

func NewCreateUseCase(repo message_shortcut.Repository) message_shortcut.CreateUseCase {
	return &createUseCase{repo: repo}
}

func (uc *createUseCase) Execute(workspaceID string, input message_shortcut.CreateInput) (*message_shortcut.MessageShortcut, error) {
	s := &message_shortcut.MessageShortcut{
		ID:          uuid.New().String(),
		WorkspaceID: workspaceID,
		Shortcut:    strings.ToLower(strings.TrimSpace(input.Shortcut)),
		Name:        strings.TrimSpace(input.Name),
		MessageType: input.MessageType,
		Content:     input.Content,
	}

	if err := s.Validate(); err != nil {
		return nil, err
	}

	exists, err := uc.repo.ShortcutExistsInWorkspace(workspaceID, s.Shortcut, nil)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, message_shortcut.ErrShortcutExists
	}

	if err := uc.repo.Create(s); err != nil {
		return nil, err
	}
	return s, nil
}
