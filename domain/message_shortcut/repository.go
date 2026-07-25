package message_shortcut

type Repository interface {
	Create(shortcut *MessageShortcut) error
	Update(shortcut *MessageShortcut) error
	Delete(id string) error
	FindByID(id string) (*MessageShortcut, error)
	FindByShortcut(workspaceID, shortcut string) (*MessageShortcut, error)
	ListByWorkspace(workspaceID string) ([]*MessageShortcut, error)
	ShortcutExistsInWorkspace(workspaceID, shortcut string, excludeID *string) (bool, error)
}
