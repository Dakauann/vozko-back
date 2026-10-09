package leadarea_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/crmfilter"
	"vozko/domain/leadarea"
	"vozko/domain/workspace"
)

var errIncomplete = errors.New("lead areas: a required dependency is missing")

type Actor = conversation.Viewer

type Permissions interface {
	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool
}

type Deps struct {
	Areas       leadarea.Repository
	Permissions Permissions
	Now         func() time.Time
	NewID       func() string
}

type Service struct {
	deps Deps
}

func New(deps Deps) (*Service, error) {
	missing := []struct {
		name   string
		absent bool
	}{
		{"areas", deps.Areas == nil},
		{"permissions", deps.Permissions == nil},
	}
	for _, m := range missing {
		if m.absent {
			return nil, fmt.Errorf("%w: %s", errIncomplete, m.name)
		}
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.NewID == nil {
		deps.NewID = uuid.NewString
	}
	return &Service{deps: deps}, nil
}

func (s *Service) allowed(a Actor) bool {
	if strings.TrimSpace(a.UserID) == "" || strings.TrimSpace(a.WorkspaceID) == "" {
		return false
	}
	for _, action := range leadarea.RequiredActions() {
		if !s.deps.Permissions.HasWorkspacePermission(a.UserID, a.WorkspaceID, string(workspace.ResourceLeads), string(action), a.IsAdmin) {
			return false
		}
	}
	return true
}

func (s *Service) authorize(a Actor) error {
	if !s.allowed(a) {
		return leadarea.ErrAddressesRequired
	}
	return nil
}

func (s *Service) Create(ctx context.Context, a Actor, d leadarea.Draft) (leadarea.Area, error) {
	if err := s.authorize(a); err != nil {
		return leadarea.Area{}, err
	}
	area, err := leadarea.New(a.WorkspaceID, a.UserID, d, s.deps.Now().UTC())
	if err != nil {
		return leadarea.Area{}, err
	}
	live, err := s.deps.Areas.CountLive(ctx, a.WorkspaceID)
	if err != nil {
		return leadarea.Area{}, fmt.Errorf("count lead areas: %w", err)
	}
	if err := leadarea.CheckRoomFor(live); err != nil {
		return leadarea.Area{}, err
	}
	area.ID = s.deps.NewID()
	if err := s.deps.Areas.Create(ctx, area); err != nil {
		return leadarea.Area{}, fmt.Errorf("save lead area: %w", err)
	}
	return area, nil
}

func (s *Service) List(ctx context.Context, a Actor) ([]leadarea.Area, error) {
	if err := s.authorize(a); err != nil {
		return nil, err
	}
	areas, err := s.deps.Areas.ListReadable(ctx, a.WorkspaceID, a.UserID)
	if err != nil {
		return nil, fmt.Errorf("list lead areas: %w", err)
	}
	readable := make([]leadarea.Area, 0, len(areas))
	for _, area := range areas {
		if area.WorkspaceID == a.WorkspaceID && area.Owned().CanRead(a.UserID) {
			readable = append(readable, area)
		}
	}
	return readable, nil
}

func (s *Service) Get(ctx context.Context, a Actor, id string) (leadarea.Area, error) {
	if err := s.authorize(a); err != nil {
		return leadarea.Area{}, err
	}
	return s.readable(ctx, a, id)
}

func (s *Service) Update(ctx context.Context, a Actor, id string, p leadarea.Patch) (leadarea.Area, error) {
	if err := s.authorize(a); err != nil {
		return leadarea.Area{}, err
	}
	current, err := s.readable(ctx, a, id)
	if err != nil {
		return leadarea.Area{}, err
	}
	next, err := current.Apply(p, a.UserID, s.deps.Now().UTC())
	if err != nil {
		return leadarea.Area{}, err
	}
	if err := s.deps.Areas.Update(ctx, next); err != nil {
		return leadarea.Area{}, err
	}
	return next, nil
}

func (s *Service) Delete(ctx context.Context, a Actor, id string) error {
	if err := s.authorize(a); err != nil {
		return err
	}
	current, err := s.readable(ctx, a, id)
	if err != nil {
		return err
	}
	if !current.Owned().CanEdit(a.UserID) {
		return leadarea.ErrForbidden
	}
	return s.deps.Areas.Delete(ctx, a.WorkspaceID, id, s.deps.Now().UTC())
}

func (s *Service) BindAreas(ctx context.Context, a Actor, f crmfilter.Filter) (crmfilter.Filter, leadarea.Stamps, error) {
	ids, err := leadarea.IDsIn(f)
	if err != nil {
		return crmfilter.Filter{}, nil, err
	}
	if len(ids) == 0 {
		return f, nil, nil
	}
	if err := s.authorize(a); err != nil {
		return crmfilter.Filter{}, nil, err
	}
	found, err := s.deps.Areas.FindLive(ctx, a.WorkspaceID, ids)
	if err != nil {
		return crmfilter.Filter{}, nil, fmt.Errorf("resolve lead areas: %w", err)
	}
	return leadarea.Bind(f, a.WorkspaceID, a.UserID, found)
}

func (s *Service) readable(ctx context.Context, a Actor, id string) (leadarea.Area, error) {
	area, err := s.deps.Areas.Get(ctx, a.WorkspaceID, id)
	if err != nil {
		return leadarea.Area{}, err
	}
	if area.WorkspaceID != a.WorkspaceID || !area.Owned().CanRead(a.UserID) {
		return leadarea.Area{}, leadarea.ErrNotFound
	}
	return area, nil
}
