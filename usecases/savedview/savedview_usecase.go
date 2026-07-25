// Package savedview_usecase implements the saved-view CRUD + set-default
// usecases. It depends only on the domain savedview.Repository port. Views are
// owned by a user and workspace-scoped; a user may only mutate their own views.
package savedview_usecase

import (
	"github.com/google/uuid"

	"vozko/domain/savedview"
)

type CreateSavedViewUseCase struct {
	repo savedview.Repository
}

func NewCreateSavedViewUseCase(repo savedview.Repository) savedview.CreateSavedViewUseCase {
	return &CreateSavedViewUseCase{repo: repo}
}

func (uc *CreateSavedViewUseCase) Execute(workspaceID, ownerID string, v *savedview.SavedView) (*savedview.SavedView, error) {
	v.ID = uuid.New().String()
	v.WorkspaceID = workspaceID
	v.OwnerID = ownerID
	v.Normalize()
	if err := v.Validate(); err != nil {
		return nil, err
	}

	if v.Position == 0 {
		existing, err := uc.repo.ListForUser(workspaceID, ownerID, v.ObjectType)
		if err != nil {
			return nil, err
		}
		maxPos := 0
		for _, e := range existing {
			if e.Position > maxPos {
				maxPos = e.Position
			}
		}
		v.Position = maxPos + 1
	}

	if err := uc.repo.Create(v); err != nil {
		return nil, err
	}
	return uc.repo.GetByID(workspaceID, v.ID)
}

type UpdateSavedViewUseCase struct {
	repo savedview.Repository
}

func NewUpdateSavedViewUseCase(repo savedview.Repository) savedview.UpdateSavedViewUseCase {
	return &UpdateSavedViewUseCase{repo: repo}
}

func (uc *UpdateSavedViewUseCase) Execute(workspaceID, ownerID, id string, patch *savedview.SavedView) (*savedview.SavedView, error) {
	existing, err := uc.repo.GetByID(workspaceID, id)
	if err != nil {
		return nil, err
	}
	if existing.OwnerID != ownerID {
		return nil, savedview.ErrUnauthorized
	}

	// Identity fields are preserved; everything else is replaced by the patch.
	patch.ID = existing.ID
	patch.WorkspaceID = workspaceID
	patch.OwnerID = existing.OwnerID
	patch.Normalize()
	if err := patch.Validate(); err != nil {
		return nil, err
	}
	if err := uc.repo.Update(patch); err != nil {
		return nil, err
	}
	return uc.repo.GetByID(workspaceID, id)
}

type DeleteSavedViewUseCase struct {
	repo savedview.Repository
}

func NewDeleteSavedViewUseCase(repo savedview.Repository) savedview.DeleteSavedViewUseCase {
	return &DeleteSavedViewUseCase{repo: repo}
}

func (uc *DeleteSavedViewUseCase) Execute(workspaceID, ownerID, id string) error {
	existing, err := uc.repo.GetByID(workspaceID, id)
	if err != nil {
		return err
	}
	if existing.OwnerID != ownerID {
		return savedview.ErrUnauthorized
	}
	return uc.repo.Delete(workspaceID, id)
}

type ListSavedViewsUseCase struct {
	repo savedview.Repository
}

func NewListSavedViewsUseCase(repo savedview.Repository) savedview.ListSavedViewsUseCase {
	return &ListSavedViewsUseCase{repo: repo}
}

func (uc *ListSavedViewsUseCase) Execute(workspaceID, ownerID string, objectType savedview.ObjectType) ([]*savedview.SavedView, error) {
	return uc.repo.ListForUser(workspaceID, ownerID, objectType)
}

type SetDefaultSavedViewUseCase struct {
	repo savedview.Repository
}

func NewSetDefaultSavedViewUseCase(repo savedview.Repository) savedview.SetDefaultSavedViewUseCase {
	return &SetDefaultSavedViewUseCase{repo: repo}
}

func (uc *SetDefaultSavedViewUseCase) Execute(workspaceID, ownerID, id string) (*savedview.SavedView, error) {
	existing, err := uc.repo.GetByID(workspaceID, id)
	if err != nil {
		return nil, err
	}
	if existing.OwnerID != ownerID {
		return nil, savedview.ErrUnauthorized
	}

	// Exclusive default: clear the user's current default for this object type
	// first, then set this one.
	if err := uc.repo.ClearDefault(workspaceID, ownerID, existing.ObjectType); err != nil {
		return nil, err
	}
	existing.IsDefault = true
	existing.Normalize()
	if err := existing.Validate(); err != nil {
		return nil, err
	}
	if err := uc.repo.Update(existing); err != nil {
		return nil, err
	}
	return uc.repo.GetByID(workspaceID, id)
}
