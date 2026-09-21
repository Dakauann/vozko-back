package savedview

type Repository interface {
	Create(v *SavedView) error
	Update(v *SavedView) error
	Delete(workspaceID, id string) error
	GetByID(workspaceID, id string) (*SavedView, error)

	ListForUser(workspaceID, userID string, objectType ObjectType) ([]*SavedView, error)

	ClearDefault(workspaceID, userID string, objectType ObjectType) error
}
