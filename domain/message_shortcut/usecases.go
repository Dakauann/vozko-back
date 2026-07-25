package message_shortcut

type CreateUseCase interface {
	Execute(workspaceID string, input CreateInput) (*MessageShortcut, error)
}

type UpdateUseCase interface {
	Execute(workspaceID, id string, input UpdateInput) (*MessageShortcut, error)
}

type DeleteUseCase interface {
	Execute(workspaceID, id string) error
}

type ListUseCase interface {
	Execute(workspaceID string) ([]*MessageShortcut, error)
}

type GetByShortcutUseCase interface {
	Execute(workspaceID, shortcut string) (*MessageShortcut, error)
}

type CreateInput struct {
	Name        string      `json:"name"`
	Shortcut    string      `json:"shortcut"`
	MessageType MessageType `json:"messageType"`
	Content     Content     `json:"content"`
}

type UpdateInput struct {
	Name        *string      `json:"name,omitempty"`
	Shortcut    *string      `json:"shortcut,omitempty"`
	MessageType *MessageType `json:"messageType,omitempty"`
	Content     *Content     `json:"content,omitempty"`
}
