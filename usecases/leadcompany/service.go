package leadcompany_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/leadcompany"
	"vozko/domain/media"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

var errIncomplete = errors.New("lead companies: a required dependency is missing")

type Actor = conversation.Viewer

type Images interface {
	GetMedia(workspaceID, mediaID string) (*media.Media, error)
}

type Deps struct {
	Companies   leadcompany.Repository
	Permissions workspace.PermissionChecker
	Images      Images
	Notifier    lead.ChangeNotifier
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
		{"companies", deps.Companies == nil},
		{"permissions", deps.Permissions == nil},
		{"images", deps.Images == nil},
		{"notifier", deps.Notifier == nil},
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

func (s *Service) Create(ctx context.Context, a Actor, d leadcompany.Draft) (leadcompany.Company, error) {
	if err := s.require(a, leadcompany.CapabilityCreate); err != nil {
		return leadcompany.Company{}, err
	}
	company, err := leadcompany.New(a.WorkspaceID, a.UserID, d, s.now())
	if err != nil {
		return leadcompany.Company{}, err
	}
	if company.ImageMediaID != "" {
		if company.Image, err = s.image(a.WorkspaceID, company.ImageMediaID); err != nil {
			return leadcompany.Company{}, err
		}
	}
	company.ID = s.deps.NewID()
	if err := s.deps.Companies.CreateCompany(ctx, company); err != nil {
		return leadcompany.Company{}, fmt.Errorf("save lead company: %w", err)
	}
	return company, nil
}

func (s *Service) Update(ctx context.Context, a Actor, id string, p leadcompany.Patch) (leadcompany.Company, error) {
	if err := s.require(a, leadcompany.CapabilityEdit); err != nil {
		return leadcompany.Company{}, err
	}
	current, err := s.deps.Companies.GetCompany(ctx, a.WorkspaceID, id)
	if err != nil {
		return leadcompany.Company{}, err
	}
	next, err := current.Apply(p, s.now())
	if err != nil {
		return leadcompany.Company{}, err
	}
	if mediaID, sets := p.NewImage(); sets {
		if next.Image, err = s.image(a.WorkspaceID, mediaID); err != nil {
			return leadcompany.Company{}, err
		}
	}
	if err := s.deps.Companies.UpdateCompany(ctx, next); err != nil {
		return leadcompany.Company{}, err
	}
	return next, nil
}

func (s *Service) Get(ctx context.Context, a Actor, id string) (leadcompany.Company, error) {
	if err := s.require(a, leadcompany.CapabilityView); err != nil {
		return leadcompany.Company{}, err
	}
	return s.deps.Companies.GetCompany(ctx, a.WorkspaceID, id)
}

func (s *Service) List(ctx context.Context, a Actor, q leadcompany.ListQuery) (*shared.PaginatedResult[leadcompany.Company], error) {
	if err := s.require(a, leadcompany.CapabilityView); err != nil {
		return nil, err
	}
	q, err := q.Normalize()
	if err != nil {
		return nil, err
	}
	return s.deps.Companies.ListCompanies(ctx, a.WorkspaceID, q)
}

func (s *Service) Delete(ctx context.Context, a Actor, id string) error {
	if err := s.require(a, leadcompany.CapabilityDelete); err != nil {
		return err
	}
	released, err := s.deps.Companies.DeleteCompany(ctx, a.WorkspaceID, id, lead.CompanyReleaseEvent(a.UserID, id), s.now())
	if err != nil {
		return err
	}
	for _, r := range released {
		s.deps.Notifier.LeadChanged(lead.Change{WorkspaceID: a.WorkspaceID, LeadID: r.LeadID, Version: r.Version, Fields: []string{lead.FieldCompany}})
	}
	return nil
}

func (s *Service) image(workspaceID, mediaID string) (*leadcompany.Image, error) {
	found, err := s.deps.Images.GetMedia(workspaceID, mediaID)
	if errors.Is(err, media.ErrMediaNotFound) {
		return nil, leadcompany.ErrImageInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("lead company image %s: %w", mediaID, err)
	}
	image, err := leadcompany.ImageOf(workspaceID, found)
	if err != nil {
		return nil, err
	}
	return &image, nil
}

func (s *Service) require(a Actor, key workspace.CapabilityKey) error {
	if strings.TrimSpace(a.WorkspaceID) == "" {
		return leadcompany.ErrWorkspaceRequired
	}
	if !workspace.HoldsCapability(s.deps.Permissions, a.WorkspaceID, a.UserID, a.IsAdmin, key) {
		return leadcompany.ErrForbidden
	}
	return nil
}

func (s *Service) now() time.Time {
	return s.deps.Now().UTC()
}
