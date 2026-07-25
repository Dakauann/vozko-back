package savedview

type CreateSavedViewUseCase interface {
	Execute(workspaceID, ownerID string, v *SavedView) (*SavedView, error)
}

type UpdateSavedViewUseCase interface {
	Execute(workspaceID, ownerID, id string, patch *SavedView) (*SavedView, error)
}

type DeleteSavedViewUseCase interface {
	Execute(workspaceID, ownerID, id string) error
}

type ListSavedViewsUseCase interface {
	Execute(workspaceID, ownerID string, objectType ObjectType) ([]*SavedView, error)
}

type SetDefaultSavedViewUseCase interface {
	Execute(workspaceID, ownerID, id string) (*SavedView, error)
}
