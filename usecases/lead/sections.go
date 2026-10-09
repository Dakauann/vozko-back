package lead_usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vozko/domain/actor"
	"vozko/domain/cache"
	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/domain/leadmap"
	"vozko/domain/workspace"
)

const (
	sectionCachePrefix = "leads:sections"
	DefaultSectionTTL  = 60 * time.Second
)

var errSectionsIncomplete = errors.New("lead sections: a required dependency is missing")

type SectionCaching struct {
	Memo     cache.Memo
	Gate     cache.Gate
	Versions cache.Versions
	TTL      time.Duration
}

type SectionDeps struct {
	Reader      lead.SectionReader
	Permissions Permissions
	Definitions DefinitionSource
	Names       actor.Namer
	Zones       WorkspaceZones
	Areas       AreaFilters
	Caching     SectionCaching
	Now         func() time.Time
}

type Sections struct {
	deps    SectionDeps
	viewers viewers
	now     func() time.Time
}

func NewSections(deps SectionDeps) (*Sections, error) {
	missing := map[string]bool{
		"reader":      deps.Reader == nil,
		"permissions": deps.Permissions == nil,
		"definitions": deps.Definitions == nil,
		"names":       deps.Names == nil,
		"zones":       deps.Zones == nil,
		"memo":        deps.Caching.Memo == nil,
		"gate":        deps.Caching.Gate == nil,
		"versions":    deps.Caching.Versions == nil,
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", errSectionsIncomplete, name)
		}
	}
	if deps.Caching.TTL <= 0 {
		deps.Caching.TTL = DefaultSectionTTL
	}
	return &Sections{
		deps:    deps,
		viewers: viewers{permissions: deps.Permissions, definitions: deps.Definitions},
		now:     clockOr(deps.Now),
	}, nil
}

func (s *Sections) Summary(ctx context.Context, a Actor, f crmfilter.Filter) (*lead.SummarySection, error) {
	return readSection(ctx, s, a, lead.SectionSummary, f, s.deps.Reader.ReadSummary)
}

func (s *Sections) Facets(ctx context.Context, a Actor, f crmfilter.Filter) (*lead.FacetsSection, error) {
	return s.FacetsColoredBy(ctx, a, f, "")
}

func (s *Sections) FacetsColoredBy(ctx context.Context, a Actor, f crmfilter.Filter, colorBy string) (*lead.FacetsSection, error) {
	return readSectionColoredBy(ctx, s, a, lead.SectionFacets, f, colorBy, func(ctx context.Context, q lead.SectionQuery) (*lead.FacetsSection, error) {
		facets, err := s.deps.Reader.ReadFacets(ctx, q)
		if err != nil {
			return nil, err
		}
		s.nameOwners(facets)
		return facets, nil
	})
}

func (s *Sections) Places(ctx context.Context, a Actor, f crmfilter.Filter) (*lead.PlacesSection, error) {
	return readSection(ctx, s, a, lead.SectionPlaces, f, s.deps.Reader.ReadPlaces)
}

func (s *Sections) nameOwners(facets *lead.FacetsSection) {
	if facets == nil || len(facets.Owners) == 0 {
		return
	}
	ids := make([]string, 0, len(facets.Owners))
	for _, o := range facets.Owners {
		ids = append(ids, o.Owner)
	}
	names := s.deps.Names.Names(ids...)
	for i := range facets.Owners {
		facets.Owners[i].Name = names[facets.Owners[i].Owner]
	}
}

func (s *Sections) query(ctx context.Context, a Actor, section lead.Section, f crmfilter.Filter) (lead.SectionQuery, lead.SectionKey, error) {
	return s.queryColoredBy(ctx, a, section, f, "")
}

func (s *Sections) queryColoredBy(ctx context.Context, a Actor, section lead.Section, f crmfilter.Filter, colorBy string) (lead.SectionQuery, lead.SectionKey, error) {
	if a.WorkspaceID == "" {
		return lead.SectionQuery{}, lead.SectionKey{}, lead.ErrLeadWorkspaceRequired
	}
	if !s.viewers.allowed(a, workspace.ActionRead) {
		return lead.SectionQuery{}, lead.SectionKey{}, lead.ErrLeadForbidden
	}
	v, err := s.viewers.of(a)
	if err != nil {
		return lead.SectionQuery{}, lead.SectionKey{}, err
	}
	colouring, err := leadmap.ColorByFor(v.Definitions, colorBy, v.Fields)
	if err != nil {
		return lead.SectionQuery{}, lead.SectionKey{}, err
	}
	binding := filterBinding{areas: s.deps.Areas, zones: s.deps.Zones, now: s.now}
	bound, err := binding.bind(ctx, a, v, f)
	if err != nil {
		return lead.SectionQuery{}, lead.SectionKey{}, err
	}
	q := lead.SectionQuery{WorkspaceID: a.WorkspaceID, Filter: bound.filter, Today: bound.today}
	if section == lead.SectionFacets {
		q.ClassificationKey = lead.ClassificationKeyFor(v)
		if colouring != nil {
			q.ClassificationKey = colouring.Key()
		}
	}
	if section == lead.SectionSummary && q.Today.IsZero() {
		if q.Today, err = binding.today(ctx, a.WorkspaceID); err != nil {
			return lead.SectionQuery{}, lead.SectionKey{}, err
		}
	}
	return q, q.Key(section, v, bound.stamps.Key()), nil
}

func readSection[T any](
	ctx context.Context,
	s *Sections,
	a Actor,
	section lead.Section,
	f crmfilter.Filter,
	read func(context.Context, lead.SectionQuery) (T, error),
) (T, error) {
	return readSectionColoredBy(ctx, s, a, section, f, "", read)
}

func readSectionColoredBy[T any](
	ctx context.Context,
	s *Sections,
	a Actor,
	section lead.Section,
	f crmfilter.Filter,
	colorBy string,
	read func(context.Context, lead.SectionQuery) (T, error),
) (T, error) {
	var zero T
	q, key, err := s.queryColoredBy(ctx, a, section, f, colorBy)
	if err != nil {
		return zero, err
	}
	return memoSection(ctx, s.deps.Caching, sectionCachePrefix, a.WorkspaceID, key, func(ctx context.Context) (T, error) {
		return read(ctx, q)
	})
}
