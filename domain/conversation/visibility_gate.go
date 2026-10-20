package conversation

import (
	"fmt"
	"strings"

	"vozko/domain/shared"
)

func RequireVisible(visibility Visibility, viewer shared.Person, workspaceID, entryID, entryType string) error {
	if visibility == nil || strings.TrimSpace(workspaceID) == "" {
		return ErrNotVisible
	}
	visible, err := visibility.CanView(viewer, workspaceID, entryID, entryType)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNotVisible, err)
	}
	if !visible {
		return ErrNotVisible
	}
	return nil
}
