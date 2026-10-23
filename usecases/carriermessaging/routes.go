package carriermessaging_usecase

import (
	"context"
	"errors"
	"strings"

	"vozko/domain/sms"
)

var ErrNumberTaken = errors.New("carrier messaging: this number already serves another sms route")

func (s *Service) CreateRoute(ctx context.Context, workspaceID, brandPrefix string) (*sms.Route, error) {
	now := s.Now()
	route := sms.Route{
		WorkspaceID: workspaceID, BrandPrefix: strings.TrimSpace(brandPrefix), Kind: sms.RouteShared,
		Status: sms.RouteActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := route.Validate(); err != nil {
		return nil, err
	}
	if err := s.Repos.Routes.Create(ctx, &route); err != nil {
		return nil, err
	}
	return &route, nil
}

func (s *Service) UpdateRoute(ctx context.Context, workspaceID, id, brandPrefix string) (*sms.Route, error) {
	route, err := s.Repos.Routes.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	route.BrandPrefix = strings.TrimSpace(brandPrefix)
	return s.saveRoute(ctx, *route)
}

func (s *Service) PauseRoute(ctx context.Context, workspaceID, id string) (*sms.Route, error) {
	route, err := s.Repos.Routes.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	route.Status, route.PauseReason = sms.RoutePaused, "manual"
	return s.saveRoute(ctx, *route)
}

func (s *Service) ResumeRoute(ctx context.Context, workspaceID, id string) (*sms.Route, error) {
	route, err := s.Repos.Routes.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	route.Status, route.PauseReason = sms.RouteActive, ""
	return s.saveRoute(ctx, *route)
}

func (s *Service) AdminAssignNumber(ctx context.Context, id, number string) (*sms.Route, error) {
	route, err := s.Repos.Routes.FindByIDUnscoped(ctx, id)
	if err != nil {
		return nil, err
	}
	number = strings.TrimSpace(number)
	if number == s.SMSSharedNumber {
		return nil, ErrNumberTaken
	}
	holder, err := s.Repos.Routes.FindByNumber(ctx, number)
	switch {
	case err == nil && holder.ID != route.ID:
		return nil, ErrNumberTaken
	case err != nil && !errors.Is(err, sms.ErrRouteNotFound):
		return nil, err
	}
	route.Kind, route.Number = sms.RouteDedicated, number
	return s.saveRoute(ctx, *route)
}

func (s *Service) AdminReleaseNumber(ctx context.Context, id string) (*sms.Route, error) {
	route, err := s.Repos.Routes.FindByIDUnscoped(ctx, id)
	if err != nil {
		return nil, err
	}
	route.Kind, route.Number = sms.RouteShared, ""
	return s.saveRoute(ctx, *route)
}

func (s *Service) ListRoutes(ctx context.Context, workspaceID string) ([]*sms.Route, error) {
	return s.Repos.Routes.ListByWorkspace(ctx, workspaceID)
}

func (s *Service) DeleteRoute(ctx context.Context, workspaceID, id string) error {
	if _, err := s.Repos.Routes.FindByID(ctx, workspaceID, id); err != nil {
		return err
	}
	return s.Repos.Routes.Delete(ctx, workspaceID, id)
}

func (s *Service) saveRoute(ctx context.Context, route sms.Route) (*sms.Route, error) {
	if err := route.Validate(); err != nil {
		return nil, err
	}
	route.UpdatedAt = s.Now()
	if err := s.Repos.Routes.Update(ctx, &route); err != nil {
		return nil, err
	}
	return &route, nil
}

func (s *Service) AdminListRoutes(ctx context.Context) ([]*sms.Route, error) {
	return s.Repos.Routes.ListAll(ctx)
}
