package lead_usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vozko/domain/actor"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

var errPagesIncomplete = errors.New("lead pages: a required dependency is missing")

type PageDeps struct {
	Leads       lead.Queries
	Contacts    lead.ContactDetails
	Permissions Permissions
	Definitions DefinitionSource
	Names       actor.Namer
	Zones       WorkspaceZones
	Areas       AreaFilters
	Now         func() time.Time
}

type Pages struct {
	deps    PageDeps
	viewers viewers
	now     func() time.Time
}

func NewPages(deps PageDeps) (*Pages, error) {
	missing := map[string]bool{
		"leads":       deps.Leads == nil,
		"contacts":    deps.Contacts == nil,
		"permissions": deps.Permissions == nil,
		"definitions": deps.Definitions == nil,
		"names":       deps.Names == nil,
		"zones":       deps.Zones == nil,
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", errPagesIncomplete, name)
		}
	}
	return &Pages{deps: deps, viewers: viewers{permissions: deps.Permissions, definitions: deps.Definitions}, now: clockOr(deps.Now)}, nil
}

func (p *Pages) prepare(ctx context.Context, a Actor, in lead.ListLeadsInput) (lead.ListLeadsInput, lead.Viewer, error) {
	if a.WorkspaceID == "" || in.WorkspaceID != a.WorkspaceID {
		return in, lead.Viewer{}, lead.ErrLeadWorkspaceRequired
	}
	if !p.viewers.allowed(a, workspace.ActionRead) {
		return in, lead.Viewer{}, lead.ErrLeadForbidden
	}
	v, err := p.viewers.of(a)
	if err != nil {
		return in, lead.Viewer{}, err
	}
	bound, err := filterBinding{areas: p.deps.Areas, zones: p.deps.Zones, now: p.now}.bind(ctx, a, v, in.Filter)
	if err != nil {
		return in, lead.Viewer{}, err
	}
	in.Filter, in.Today = bound.filter, bound.today
	return in, v, nil
}

func (p *Pages) List(ctx context.Context, a Actor, in lead.ListLeadsInput) (*shared.PaginatedResult[*lead.LeadWithSummary], error) {
	in, v, err := p.prepare(ctx, a, in)
	if err != nil {
		return nil, err
	}
	page, err := p.deps.Leads.List(in)
	if err != nil {
		return nil, err
	}
	leads := make([]*lead.Lead, 0, len(page.Items))
	for _, item := range page.Items {
		leads = append(leads, item.Lead)
	}
	if err := p.deps.Contacts.AttachContactDetails(ctx, a.WorkspaceID, leads); err != nil {
		return nil, fmt.Errorf("contact details of the lead page: %w", err)
	}
	for _, item := range page.Items {
		item.Lead = lead.VisibleFields(item.Lead, v)
	}
	p.nameOwners(page.Items)
	return page, nil
}

func (p *Pages) nameOwners(items []*lead.LeadWithSummary) {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.Lead != nil && item.Lead.Owner != "" {
			ids = append(ids, item.Lead.Owner)
		}
	}
	if len(ids) == 0 {
		return
	}
	names := p.deps.Names.Names(ids...)
	for _, item := range items {
		if item.Lead != nil {
			item.OwnerName = names[item.Lead.Owner]
		}
	}
}
