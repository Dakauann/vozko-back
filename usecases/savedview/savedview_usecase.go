package savedview_usecase

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/savedview"
)

type Permissions interface {
	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool
}

type LeadFilters interface {
	CheckLeadFilter(a savedview.Actor, f crmfilter.Filter) error
}

type Access struct {
	Permissions Permissions
	LeadFilters LeadFilters
}

func (acc Access) require(a savedview.Actor, object savedview.ObjectType, op savedview.Operation) error {
	need, err := savedview.RequiredPermission(object, op)
	if err != nil {
		return err
	}
	if acc.Permissions == nil || strings.TrimSpace(a.UserID) == "" || strings.TrimSpace(a.WorkspaceID) == "" ||
		!acc.Permissions.HasWorkspacePermission(a.UserID, a.WorkspaceID, string(need.Resource), string(need.Action), a.IsAdmin) {
		return fmt.Errorf("%w: %s", savedview.ErrForbidden, need.Key())
	}
	return nil
}

func (acc Access) checkFilter(a savedview.Actor, v *savedview.SavedView) error {
	if v.ObjectType != savedview.ObjectLead {
		return nil
	}
	if acc.LeadFilters == nil {
		return fmt.Errorf("%w: lead filters cannot be checked", savedview.ErrForbidden)
	}
	return acc.LeadFilters.CheckLeadFilter(a, v.Filter)
}

func (acc Access) readableBy(a savedview.Actor, v *savedview.SavedView) (bool, error) {
	if !v.Owned().CanRead(a.UserID) {
		return false, nil
	}
	if v.OwnerID == a.UserID {
		return true, nil
	}
	err := acc.checkFilter(a, v)
	switch {
	case err == nil:
		return true, nil
	case hidesTheView(err):
		return false, nil
	}
	return false, err
}

func hidesTheView(err error) bool {
	return errors.Is(err, customfield.ErrFilterSensitive) ||
		errors.Is(err, lead.ErrLeadFilterAddressForbidden) ||
		errors.Is(err, lead.ErrLeadFilterInvalid)
}

func (acc Access) load(repo savedview.Repository, a savedview.Actor, id string, op savedview.Operation) (*savedview.SavedView, error) {
	existing, err := repo.GetByID(a.WorkspaceID, id)
	if err != nil {
		return nil, err
	}
	if err := acc.require(a, existing.ObjectType, op); err != nil {
		return nil, err
	}
	if !existing.Owned().CanEdit(a.UserID) {
		return nil, savedview.ErrUnauthorized
	}
	return existing, nil
}

type CreateSavedViewUseCase struct {
	repo   savedview.Repository
	access Access
}

func NewCreateSavedViewUseCase(repo savedview.Repository, access Access) savedview.CreateSavedViewUseCase {
	return &CreateSavedViewUseCase{repo: repo, access: access}
}

func (uc *CreateSavedViewUseCase) Execute(a savedview.Actor, v *savedview.SavedView) (*savedview.SavedView, error) {
	v.ID = uuid.New().String()
	v.WorkspaceID = a.WorkspaceID
	v.OwnerID = a.UserID
	v.Normalize()
	if err := v.Validate(); err != nil {
		return nil, err
	}
	if err := uc.access.require(a, v.ObjectType, savedview.OperationCreate); err != nil {
		return nil, err
	}
	if err := uc.access.checkFilter(a, v); err != nil {
		return nil, err
	}

	if v.Position == 0 {
		existing, err := uc.repo.ListForUser(a.WorkspaceID, a.UserID, v.ObjectType)
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
	return uc.repo.GetByID(a.WorkspaceID, v.ID)
}

type UpdateSavedViewUseCase struct {
	repo   savedview.Repository
	access Access
}

func NewUpdateSavedViewUseCase(repo savedview.Repository, access Access) savedview.UpdateSavedViewUseCase {
	return &UpdateSavedViewUseCase{repo: repo, access: access}
}

func (uc *UpdateSavedViewUseCase) Execute(a savedview.Actor, id string, patch *savedview.SavedView) (*savedview.SavedView, error) {
	existing, err := uc.access.load(uc.repo, a, id, savedview.OperationUpdate)
	if err != nil {
		return nil, err
	}

	patch.ID = existing.ID
	patch.WorkspaceID = a.WorkspaceID
	patch.OwnerID = existing.OwnerID
	patch.Normalize()
	if err := patch.Validate(); err != nil {
		return nil, err
	}
	if err := uc.access.require(a, patch.ObjectType, savedview.OperationUpdate); err != nil {
		return nil, err
	}
	if err := uc.access.checkFilter(a, patch); err != nil {
		return nil, err
	}
	if err := uc.repo.Update(patch); err != nil {
		return nil, err
	}
	return uc.repo.GetByID(a.WorkspaceID, id)
}

type DeleteSavedViewUseCase struct {
	repo   savedview.Repository
	access Access
}

func NewDeleteSavedViewUseCase(repo savedview.Repository, access Access) savedview.DeleteSavedViewUseCase {
	return &DeleteSavedViewUseCase{repo: repo, access: access}
}

func (uc *DeleteSavedViewUseCase) Execute(a savedview.Actor, id string) error {
	if _, err := uc.access.load(uc.repo, a, id, savedview.OperationDelete); err != nil {
		return err
	}
	return uc.repo.Delete(a.WorkspaceID, id)
}

type ListSavedViewsUseCase struct {
	repo   savedview.Repository
	access Access
}

func NewListSavedViewsUseCase(repo savedview.Repository, access Access) savedview.ListSavedViewsUseCase {
	return &ListSavedViewsUseCase{repo: repo, access: access}
}

func (uc *ListSavedViewsUseCase) Execute(a savedview.Actor, objectType savedview.ObjectType) ([]*savedview.SavedView, error) {
	if err := uc.access.require(a, objectType, savedview.OperationRead); err != nil {
		return nil, err
	}
	views, err := uc.repo.ListForUser(a.WorkspaceID, a.UserID, objectType)
	if err != nil {
		return nil, err
	}
	readable := make([]*savedview.SavedView, 0, len(views))
	for _, v := range views {
		ok, err := uc.access.readableBy(a, v)
		if err != nil {
			return nil, err
		}
		if ok {
			readable = append(readable, v)
		}
	}
	return readable, nil
}

type SetDefaultSavedViewUseCase struct {
	repo   savedview.Repository
	access Access
}

func NewSetDefaultSavedViewUseCase(repo savedview.Repository, access Access) savedview.SetDefaultSavedViewUseCase {
	return &SetDefaultSavedViewUseCase{repo: repo, access: access}
}

func (uc *SetDefaultSavedViewUseCase) Execute(a savedview.Actor, id string) (*savedview.SavedView, error) {
	existing, err := uc.access.load(uc.repo, a, id, savedview.OperationUpdate)
	if err != nil {
		return nil, err
	}

	if err := uc.repo.ClearDefault(a.WorkspaceID, a.UserID, existing.ObjectType); err != nil {
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
	return uc.repo.GetByID(a.WorkspaceID, id)
}
