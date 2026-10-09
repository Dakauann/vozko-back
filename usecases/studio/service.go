package studio_usecase

import (
	"context"
	"encoding/json"
	"errors"

	"vozko/domain/shared"
	"vozko/domain/studio"
)

var ErrMissingDependency = errors.New("studio: the service is missing a dependency")

type Service struct {
	projects studio.Repository
}

func NewService(projects studio.Repository) (*Service, error) {
	if projects == nil {
		return nil, ErrMissingDependency
	}
	return &Service{projects: projects}, nil
}

func (s *Service) Create(ctx context.Context, workspaceID, userID string, kind studio.Kind, name string, document json.RawMessage) (*studio.Project, error) {
	p, err := studio.NewProject(workspaceID, userID, kind, name, document)
	if err != nil {
		return nil, err
	}
	if err := s.projects.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) List(ctx context.Context, q studio.ListQuery) ([]studio.Summary, int64, error) {
	return s.projects.List(ctx, q)
}

func (s *Service) Get(ctx context.Context, workspaceID, id string) (*studio.Project, error) {
	return s.projects.Get(ctx, workspaceID, id)
}

func (s *Service) Save(ctx context.Context, workspaceID, id string, expectedVersion int64, change studio.Change) (*studio.Project, error) {
	p, err := s.loadAt(ctx, workspaceID, id, expectedVersion)
	if err != nil {
		return nil, err
	}
	if err := p.Apply(change); err != nil {
		return nil, err
	}
	if err := s.projects.Save(ctx, p, expectedVersion); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) Archive(ctx context.Context, workspaceID, id string) error {
	return s.projects.Archive(ctx, workspaceID, id)
}

func (s *Service) loadAt(ctx context.Context, workspaceID, id string, version int64) (*studio.Project, error) {
	p, err := s.projects.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if err := shared.ExpectVersion(p.Version, &version); err != nil {
		return nil, err
	}
	return p, nil
}
