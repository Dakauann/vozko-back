package scheduled_message_usecase

import (
	"errors"
	"fmt"

	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

type templatePermission struct {
	access workspace.CheckAccessUseCase
}

func (p templatePermission) require(by shared.Person, workspaceID string) error {
	if by.SystemAdmin {
		return nil
	}
	err := p.access.Execute(by.UserID, workspaceID, workspace.ResourceWhatsAppTemplates, workspace.ActionSend)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, workspace.ErrInsufficientPermissions), errors.Is(err, workspace.ErrUnauthorized):
		return sm.ErrTemplatePermission
	default:
		return fmt.Errorf("scheduled message: could not read the template permission: %w", err)
	}
}
