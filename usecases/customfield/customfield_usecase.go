package customfield_usecase

import (
	"errors"

	"github.com/google/uuid"

	"vozko/domain/customfield"
)

var ErrKeyExists = errors.New("customfield: key already exists for this object")

type Service struct {
	repo customfield.Repository
}

func NewService(repo customfield.Repository) *Service {
	return &Service{repo: repo}
}

type CreateInput struct {
	ObjectType string
	Key        string
	Label      string
	Type       customfield.FieldType
	Options    []string
	Required   bool
	Position   int
}

func (s *Service) Create(workspaceID string, in CreateInput) (*customfield.Definition, error) {
	d := &customfield.Definition{
		ID:          uuid.New().String(),
		WorkspaceID: workspaceID,
		ObjectType:  in.ObjectType,
		Key:         in.Key,
		Label:       in.Label,
		Type:        in.Type,
		Options:     in.Options,
		Required:    in.Required,
		Position:    in.Position,
	}
	d.Normalize()
	if err := d.Validate(); err != nil {
		return nil, err
	}

	existing, err := s.repo.ListByObject(workspaceID, d.ObjectType)
	if err != nil {
		return nil, err
	}
	for _, e := range existing {
		if e.Key == d.Key {
			return nil, ErrKeyExists
		}
	}

	if err := s.repo.Create(d); err != nil {
		return nil, err
	}
	return s.repo.GetByID(workspaceID, d.ID)
}

type UpdateInput struct {
	Label    *string
	Type     *customfield.FieldType
	Options  []string
	Required *bool
	Position *int
}

func (s *Service) Update(workspaceID, id string, in UpdateInput) (*customfield.Definition, error) {
	d, err := s.repo.GetByID(workspaceID, id)
	if err != nil {
		return nil, err
	}
	if in.Label != nil {
		d.Label = *in.Label
	}
	if in.Type != nil {
		d.Type = *in.Type
	}
	if in.Options != nil {
		d.Options = in.Options
	}
	if in.Required != nil {
		d.Required = *in.Required
	}
	if in.Position != nil {
		d.Position = *in.Position
	}
	d.Normalize()
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.Update(d); err != nil {
		return nil, err
	}
	return s.repo.GetByID(workspaceID, id)
}

func (s *Service) Delete(workspaceID, id string) error {
	return s.repo.Delete(workspaceID, id)
}

func (s *Service) Get(workspaceID, id string) (*customfield.Definition, error) {
	return s.repo.GetByID(workspaceID, id)
}

func (s *Service) ListByObject(workspaceID, objectType string) ([]*customfield.Definition, error) {
	return s.repo.ListByObject(workspaceID, objectType)
}
