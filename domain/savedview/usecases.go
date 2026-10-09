package savedview

type CreateSavedViewUseCase interface {
	Execute(a Actor, v *SavedView) (*SavedView, error)
}

type UpdateSavedViewUseCase interface {
	Execute(a Actor, id string, patch *SavedView) (*SavedView, error)
}

type DeleteSavedViewUseCase interface {
	Execute(a Actor, id string) error
}

type ListSavedViewsUseCase interface {
	Execute(a Actor, objectType ObjectType) ([]*SavedView, error)
}

type SetDefaultSavedViewUseCase interface {
	Execute(a Actor, id string) (*SavedView, error)
}
