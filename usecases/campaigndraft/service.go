package campaigndraft_usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/campaigndraft"
)

type Scope struct {
	WorkspaceID string
	UserID      string
}

type Service struct {
	repo  campaigndraft.Repository
	now   func() time.Time
	newID func() string
}

func New(repo campaigndraft.Repository) (*Service, error) {
	if repo == nil {
		return nil, fmt.Errorf("campaign drafts: the repository is required")
	}
	return &Service{repo: repo, now: time.Now, newID: uuid.NewString}, nil
}

func (s *Service) scoped(scope Scope) error {
	if strings.TrimSpace(scope.WorkspaceID) == "" || strings.TrimSpace(scope.UserID) == "" {
		return fmt.Errorf("%w: workspace and person are required", campaigndraft.ErrInvalid)
	}
	return nil
}

func (s *Service) List(ctx context.Context, scope Scope, ch campaigndraft.Channel) ([]campaigndraft.Draft, error) {
	if err := s.scoped(scope); err != nil {
		return nil, err
	}
	if !ch.Valid() {
		return nil, campaigndraft.ErrInvalid
	}
	return s.repo.List(ctx, scope.WorkspaceID, ch, campaigndraft.ListLimit)
}

func (s *Service) Create(ctx context.Context, scope Scope, ch campaigndraft.Channel, name string, payload json.RawMessage) (*campaigndraft.Draft, error) {
	if err := s.scoped(scope); err != nil {
		return nil, err
	}
	d, err := campaigndraft.New(s.newID(), scope.WorkspaceID, scope.UserID, ch, name, payload, s.now().UTC())
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Service) Get(ctx context.Context, scope Scope, ch campaigndraft.Channel, id string) (*campaigndraft.Draft, error) {
	if err := s.scoped(scope); err != nil {
		return nil, err
	}
	if !ch.Valid() || strings.TrimSpace(id) == "" {
		return nil, campaigndraft.ErrNotFound
	}
	return s.repo.Find(ctx, scope.WorkspaceID, ch, strings.TrimSpace(id))
}

func (s *Service) Update(ctx context.Context, scope Scope, ch campaigndraft.Channel, id, name string, payload json.RawMessage) (*campaigndraft.Draft, error) {
	d, err := s.Get(ctx, scope, ch, id)
	if err != nil {
		return nil, err
	}
	if err := d.Revise(name, payload, s.now().UTC()); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Service) Delete(ctx context.Context, scope Scope, ch campaigndraft.Channel, id string) error {
	if err := s.scoped(scope); err != nil {
		return err
	}
	if !ch.Valid() || strings.TrimSpace(id) == "" {
		return campaigndraft.ErrNotFound
	}
	return s.repo.Delete(ctx, scope.WorkspaceID, ch, strings.TrimSpace(id))
}
