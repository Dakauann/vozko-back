package dialer

type Repository interface {
	Create(session *Session) error
	Update(session *Session) error
	FindByID(sessionID string) (*Session, error)
	FindActiveByOwnerConnection(workspaceID, connectionID string) (*Session, error)
	ListActiveByWorkspace(workspaceID string) ([]*Session, error)
}
