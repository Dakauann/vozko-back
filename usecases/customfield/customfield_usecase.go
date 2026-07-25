// Package customfield_usecase implements CRUD for workspace custom field
// definitions. It depends only on the domain customfield.Repository port. Keys
// are unique per (workspace, object_type); the usecase pre-checks for a clean
// conflict error in addition to the DB unique index.
package customfield_usecase

import (
	"errors"

	"github.com/google/uuid"

	"vozko/domain/customfield"
)

// ErrKeyExists is returned when a definition with the same key already exists for
// the workspace + object type.
var ErrKeyExists = errors.New("customfield: key already exists for this object")

// Service is the custom field definition usecase surface.
type Service struct {
	repo customfield.Repository
}

// NewService wires the custom field usecases from the domain port.
func NewService(repo customfield.Repository) *Service {
	return &Service{repo: repo}
}

// CreateInput is the payload to define a custom field.
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

// UpdateInput patches a definition. The object type and key are immutable
// (identity); everything else may change.
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
