package savedview

// Repository is the persistence port for saved views. It lives in the domain so
// usecases depend on this interface, not on the GORM implementation in infra.
type Repository interface {
	Create(v *SavedView) error
	Update(v *SavedView) error
	Delete(workspaceID, id string) error
	GetByID(workspaceID, id string) (*SavedView, error)

	// ListForUser returns the user's private views plus every shared view in the
	// workspace for the given object type, ordered by Position.
	ListForUser(workspaceID, userID string, objectType ObjectType) ([]*SavedView, error)

	// ClearDefault unsets IsDefault on the user's views for an object type so a
	// newly-set default stays exclusive.
	ClearDefault(workspaceID, userID string, objectType ObjectType) error
}
