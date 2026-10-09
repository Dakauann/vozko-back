package customfield_usecase

import (
	"fmt"
	"maps"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/customfield"
)

var ErrKeyExists = customfield.ErrKeyExists

type Permissions interface {
	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool
}

type Actor = conversation.Viewer

type Service struct {
	repo        customfield.Store
	permissions Permissions
}

func NewService(repo customfield.Store, permissions Permissions) *Service {
	return &Service{repo: repo, permissions: permissions}
}

func (s *Service) require(a Actor, object customfield.ObjectType, op customfield.Operation, sensitive bool) error {
	if strings.TrimSpace(a.WorkspaceID) == "" {
		return customfield.ErrWorkspaceRequired
	}
	needs, err := customfield.RequiredPermissions(object, op, sensitive)
	if err != nil {
		return err
	}
	for _, need := range needs {
		if s.permissions == nil || strings.TrimSpace(a.UserID) == "" ||
			!s.permissions.HasWorkspacePermission(a.UserID, a.WorkspaceID, string(need.Resource), string(need.Action), a.IsAdmin) {
			return fmt.Errorf("%w: %s", customfield.ErrForbidden, need.Key())
		}
	}
	return nil
}

func (s *Service) load(a Actor, id string, op customfield.Operation) (*customfield.Definition, error) {
	if strings.TrimSpace(a.WorkspaceID) == "" {
		return nil, customfield.ErrWorkspaceRequired
	}
	d, err := s.repo.GetByID(a.WorkspaceID, id)
	if err != nil {
		return nil, err
	}
	if err := s.require(a, d.ObjectType, op, customfield.TouchesSensitiveData(d, nil)); err != nil {
		return nil, err
	}
	return d, nil
}

type CreateInput struct {
	ObjectType  customfield.ObjectType
	Key         string
	Label       string
	Type        customfield.FieldType
	Options     []string
	OptionTones map[string]customfield.Tone
	Required    bool
	Sensitive   *bool
	LegalBasis  string
	Role        customfield.Role
	Position    int
}

func (s *Service) Create(a Actor, in CreateInput) (*customfield.Definition, error) {
	workspaceID := a.WorkspaceID
	d := &customfield.Definition{
		ID:          uuid.New().String(),
		WorkspaceID: workspaceID,
		ObjectType:  in.ObjectType,
		Key:         in.Key,
		Label:       in.Label,
		Type:        in.Type,
		Options:     in.Options,
		OptionTones: maps.Clone(in.OptionTones),
		Required:    in.Required,
		LegalBasis:  in.LegalBasis,
		Role:        in.Role,
		Position:    in.Position,
	}
	if in.Sensitive != nil {
		d.Sensitive = *in.Sensitive
	}
	d.Normalize()
	if err := s.require(a, d.ObjectType, customfield.OperationCreate, customfield.TouchesSensitiveData(nil, d)); err != nil {
		return nil, err
	}
	retired, err := s.repo.RetiredByKey(workspaceID, d.ObjectType, d.Key)
	if err != nil {
		return nil, err
	}
	if err := s.require(a, d.ObjectType, customfield.OperationCreate, customfield.CreationTouchesSensitiveData(retired, d)); err != nil {
		return nil, err
	}
	if in.Sensitive == nil && d.ObjectType.RequiresSensitivityChoice() {
		return nil, customfield.ErrSensitivityChoiceMissing
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if err := s.checkUnique(d); err != nil {
		return nil, err
	}
	if err := s.repo.Create(d); err != nil {
		return nil, err
	}
	return s.repo.GetByID(workspaceID, d.ID)
}

type UpdateInput struct {
	Label       *string
	Type        *customfield.FieldType
	Options     []string
	OptionTones map[string]customfield.Tone
	Required    *bool
	Sensitive   *bool
	LegalBasis  *string
	Role        *customfield.Role
	Position    *int
}

func (s *Service) Update(a Actor, id string, in UpdateInput) (*customfield.Definition, error) {
	workspaceID := a.WorkspaceID
	d, err := s.load(a, id, customfield.OperationUpdate)
	if err != nil {
		return nil, err
	}
	before := *d
	applyUpdate(d, in)
	d.Normalize()
	if err := s.require(a, d.ObjectType, customfield.OperationUpdate, customfield.TouchesSensitiveData(&before, d)); err != nil {
		return nil, err
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if err := s.checkUnique(d); err != nil {
		return nil, err
	}
	if err := s.repo.Update(d); err != nil {
		return nil, err
	}
	return s.repo.GetByID(workspaceID, id)
}

func applyUpdate(d *customfield.Definition, in UpdateInput) {
	if in.Label != nil {
		d.Label = *in.Label
	}
	if in.Type != nil {
		d.Type = *in.Type
	}
	if in.Options != nil {
		d.ReplaceOptions(in.Options)
	}
	if in.OptionTones != nil {
		d.OptionTones = maps.Clone(in.OptionTones)
	}
	if in.Required != nil {
		d.Required = *in.Required
	}
	if in.Sensitive != nil {
		d.Sensitive = *in.Sensitive
	}
	if in.LegalBasis != nil {
		d.LegalBasis = *in.LegalBasis
	}
	if in.Role != nil {
		d.Role = *in.Role
	}
	if in.Position != nil {
		d.Position = *in.Position
	}
}

func (s *Service) checkUnique(d *customfield.Definition) error {
	existing, err := s.repo.ListByObject(d.WorkspaceID, d.ObjectType)
	if err != nil {
		return err
	}
	for _, e := range existing {
		if e.ID == d.ID {
			continue
		}
		if e.Key == d.Key {
			return customfield.ErrKeyExists
		}
		if d.Role != "" && e.Role == d.Role {
			return customfield.ErrRoleTaken
		}
	}
	return nil
}

func (s *Service) Delete(a Actor, id string) error {
	if _, err := s.load(a, id, customfield.OperationDelete); err != nil {
		return err
	}
	return s.repo.Delete(a.WorkspaceID, id)
}

func (s *Service) Get(a Actor, id string) (*customfield.Definition, error) {
	return s.load(a, id, customfield.OperationRead)
}

func (s *Service) ListByObject(a Actor, objectType customfield.ObjectType) ([]*customfield.Definition, error) {
	if err := s.require(a, objectType, customfield.OperationRead, false); err != nil {
		return nil, err
	}
	return s.repo.ListByObject(a.WorkspaceID, objectType)
}
