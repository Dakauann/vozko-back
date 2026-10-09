package lead_usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vozko/domain/cache"
	"vozko/domain/crmfilter"
	"vozko/domain/geo"
	"vozko/domain/lead"
	"vozko/domain/leadarea"
	"vozko/domain/leadmap"
)

const mapCachePrefix = "leads:map"

var (
	errMapIncomplete    = errors.New("lead map: a required dependency is missing")
	errAreasUnavailable = fmt.Errorf("%w: area filters are not available on this server", lead.ErrLeadFilterInvalid)
)

type AreaFilters interface {
	BindAreas(ctx context.Context, a Actor, f crmfilter.Filter) (crmfilter.Filter, leadarea.Stamps, error)
}

type LeadsByID interface {
	FindByIDs(workspaceID string, ids []string) ([]*lead.Lead, error)
}

type MapDeps struct {
	Reader      leadmap.Reader
	Areas       AreaFilters
	Leads       LeadsByID
	Contacts    lead.ContactDetails
	Permissions Permissions
	Definitions DefinitionSource
	Zones       WorkspaceZones
	Caching     SectionCaching
	Now         func() time.Time
}

type MapSections struct {
	deps    MapDeps
	viewers viewers
	now     func() time.Time
}

type LayerQuery struct {
	BBox    geo.BBox
	Zoom    float64
	ColorBy string
}

type TileQuery struct {
	Z, X, Y int
	ColorBy string
}

type MapPeek struct {
	Total int
	Leads []*lead.Lead
}

type mapScope struct {
	scope  leadmap.Scope
	viewer lead.Viewer
	areas  leadarea.Stamps
}

func NewMapSections(deps MapDeps) (*MapSections, error) {
	missing := []struct {
		name   string
		absent bool
	}{
		{"reader", deps.Reader == nil},
		{"areas", deps.Areas == nil},
		{"leads", deps.Leads == nil},
		{"contacts", deps.Contacts == nil},
		{"permissions", deps.Permissions == nil},
		{"definitions", deps.Definitions == nil},
		{"zones", deps.Zones == nil},
		{"memo", deps.Caching.Memo == nil},
		{"gate", deps.Caching.Gate == nil},
		{"versions", deps.Caching.Versions == nil},
	}
	for _, m := range missing {
		if m.absent {
			return nil, fmt.Errorf("%w: %s", errMapIncomplete, m.name)
		}
	}
	if deps.Caching.TTL <= 0 {
		deps.Caching.TTL = DefaultSectionTTL
	}
	return &MapSections{
		deps:    deps,
		viewers: viewers{permissions: deps.Permissions, definitions: deps.Definitions},
		now:     clockOr(deps.Now),
	}, nil
}

func (s *MapSections) Summary(ctx context.Context, a Actor, f crmfilter.Filter) (leadmap.Summary, error) {
	m, err := s.prepare(ctx, a, f)
	if err != nil {
		return leadmap.Summary{}, err
	}
	return remember(ctx, s, m, leadmap.SectionSummary, nil, func(ctx context.Context) (leadmap.Summary, error) {
		return s.deps.Reader.Summary(ctx, m.scope)
	})
}

func (s *MapSections) Layer(ctx context.Context, a Actor, f crmfilter.Filter, q LayerQuery) (leadmap.Layer, error) {
	m, err := s.prepare(ctx, a, f)
	if err != nil {
		return leadmap.Layer{}, err
	}
	window, err := geo.SnapWindow(q.BBox, q.Zoom)
	if err != nil {
		return leadmap.Layer{}, err
	}
	colorBy, err := leadmap.ColorByFor(m.viewer.Definitions, q.ColorBy, m.viewer.Fields)
	if err != nil {
		return leadmap.Layer{}, err
	}
	return s.draw(ctx, m, leadmap.SectionLayer, window.Key(), colorBy, leadmap.NewLayerRequest(window, colorBy))
}

func (s *MapSections) Tile(ctx context.Context, a Actor, f crmfilter.Filter, q TileQuery) (leadmap.Layer, error) {
	m, err := s.prepare(ctx, a, f)
	if err != nil {
		return leadmap.Layer{}, err
	}
	tile, err := geo.TileAt(q.Z, q.X, q.Y)
	if err != nil {
		return leadmap.Layer{}, err
	}
	colorBy, err := leadmap.ColorByFor(m.viewer.Definitions, q.ColorBy, m.viewer.Fields)
	if err != nil {
		return leadmap.Layer{}, err
	}
	return s.draw(ctx, m, leadmap.SectionTile, tile.Key(), colorBy, leadmap.NewTileRequest(tile, colorBy))
}

func (s *MapSections) draw(ctx context.Context, m mapScope, section leadmap.Section, place string, colorBy *leadmap.ColorBy, request leadmap.LayerRequest) (leadmap.Layer, error) {
	layer, err := remember(ctx, s, m, section, []string{place, colorBy.Key()}, func(ctx context.Context) (leadmap.Layer, error) {
		return s.deps.Reader.Layer(ctx, m.scope, request)
	})
	if err != nil {
		return leadmap.Layer{}, err
	}
	for i := range layer.Points {
		layer.Points[i].Tone = colorBy.ToneOf(layer.Points[i].Value)
	}
	return layer, nil
}

func (s *MapSections) Districts(ctx context.Context, a Actor, f crmfilter.Filter) ([]leadmap.District, error) {
	m, err := s.prepare(ctx, a, f)
	if err != nil {
		return nil, err
	}
	return remember(ctx, s, m, leadmap.SectionDistricts, nil, func(ctx context.Context) ([]leadmap.District, error) {
		districts, err := s.deps.Reader.Districts(ctx, m.scope, leadmap.MaxDistricts)
		if err != nil {
			return nil, err
		}
		return leadmap.Drawable(districts), nil
	})
}

func (s *MapSections) Viewport(ctx context.Context, a Actor, f crmfilter.Filter) (leadmap.Viewport, error) {
	m, err := s.prepare(ctx, a, f)
	if err != nil {
		return leadmap.Viewport{}, err
	}
	return remember(ctx, s, m, leadmap.SectionViewport, nil, func(ctx context.Context) (leadmap.Viewport, error) {
		summary, err := s.deps.Reader.Summary(ctx, m.scope)
		if err != nil {
			return leadmap.Viewport{}, err
		}
		if area, ok := leadmap.AreaViewport(m.scope.Filter.BoundAreas(), summary); ok {
			return area, nil
		}
		extent, err := s.deps.Reader.Extent(ctx, m.scope)
		if err != nil {
			return leadmap.Viewport{}, err
		}
		var city *leadmap.CityPlace
		if extent == nil {
			if city, err = s.deps.Reader.TopCity(ctx, m.scope); err != nil {
				return leadmap.Viewport{}, err
			}
		}
		return leadmap.DefaultViewport(extent, city, summary), nil
	})
}

func (s *MapSections) LeftOut(ctx context.Context, a Actor, f crmfilter.Filter) (leadmap.LeftOut, error) {
	m, err := s.prepare(ctx, a, f)
	if err != nil {
		return leadmap.LeftOut{}, err
	}
	left, ok, err := leadarea.LeftOut(m.scope.Filter)
	if err != nil {
		return leadmap.LeftOut{}, err
	}
	if !ok {
		return leadmap.NoneLeftOut(), nil
	}
	m.scope.Filter = left
	out, err := remember(ctx, s, m, leadmap.SectionLeftOut, nil, func(ctx context.Context) (leadmap.LeftOut, error) {
		return s.deps.Reader.LeftOut(ctx, m.scope, leadmap.MaxLeftOutDistricts)
	})
	if err != nil {
		return leadmap.LeftOut{}, err
	}
	if out.Districts == nil {
		out.Districts = []lead.DistrictCount{}
	}
	return out, nil
}

func (s *MapSections) PointLeads(ctx context.Context, a Actor, f crmfilter.Filter, at geo.Point, placement crmfilter.GeoPlacement) (MapPeek, error) {
	m, err := s.prepare(ctx, a, f)
	if err != nil {
		return MapPeek{}, err
	}
	if at.Validate() != nil {
		return MapPeek{}, leadmap.ErrInvalidPosition
	}
	if placement != crmfilter.PlacementOnMap && placement != crmfilter.PlacementApproximate {
		return MapPeek{}, leadmap.ErrInvalidPlacement
	}
	var ids []string
	var total int
	err = cache.Gated(ctx, s.deps.Caching.Gate, func(ctx context.Context) error {
		var err error
		ids, total, err = s.deps.Reader.LeadsAt(ctx, m.scope, at, placement, leadmap.MaxPeekLeads)
		return err
	})
	if err != nil {
		return MapPeek{}, err
	}
	if len(ids) == 0 {
		return MapPeek{Total: total, Leads: []*lead.Lead{}}, nil
	}
	found, err := s.deps.Leads.FindByIDs(a.WorkspaceID, ids)
	if err != nil {
		return MapPeek{}, fmt.Errorf("leads at the position: %w", err)
	}
	if err := s.deps.Contacts.AttachContactDetails(ctx, a.WorkspaceID, found); err != nil {
		return MapPeek{}, fmt.Errorf("contact details of the leads at the position: %w", err)
	}
	byID := make(map[string]*lead.Lead, len(found))
	for _, l := range found {
		if l != nil && l.WorkspaceID == a.WorkspaceID {
			byID[l.ID] = l
		}
	}
	leads := make([]*lead.Lead, 0, len(ids))
	for _, id := range ids {
		if l, ok := byID[id]; ok {
			leads = append(leads, lead.VisibleFields(l, m.viewer))
		}
	}
	return MapPeek{Total: total, Leads: leads}, nil
}

func (s *MapSections) prepare(ctx context.Context, a Actor, f crmfilter.Filter) (mapScope, error) {
	if a.WorkspaceID == "" {
		return mapScope{}, lead.ErrLeadWorkspaceRequired
	}
	for _, action := range leadarea.RequiredActions() {
		if !s.viewers.allowed(a, action) {
			return mapScope{}, lead.ErrLeadForbidden
		}
	}
	v, err := s.viewers.of(a)
	if err != nil {
		return mapScope{}, err
	}
	bound, err := filterBinding{areas: s.deps.Areas, zones: s.deps.Zones, now: s.now}.bind(ctx, a, v, f)
	if err != nil {
		return mapScope{}, err
	}
	scope := leadmap.Scope{WorkspaceID: a.WorkspaceID, Filter: bound.filter, Today: bound.today}
	return mapScope{scope: scope, viewer: v, areas: bound.stamps}, nil
}

func remember[T any](
	ctx context.Context,
	s *MapSections,
	m mapScope,
	section leadmap.Section,
	parts []string,
	read func(context.Context) (T, error),
) (T, error) {
	key := m.scope.Key(lead.Section(section), m.viewer, m.areas.Key(), parts...)
	return memoSection(ctx, s.deps.Caching, mapCachePrefix, m.scope.WorkspaceID, key, read)
}

func bindLeadAreas(ctx context.Context, areas AreaFilters, a Actor, f crmfilter.Filter) (crmfilter.Filter, leadarea.Stamps, error) {
	if areas != nil {
		return areas.BindAreas(ctx, a, f)
	}
	ids, err := leadarea.IDsIn(f)
	if err != nil {
		return crmfilter.Filter{}, nil, err
	}
	if len(ids) > 0 {
		return crmfilter.Filter{}, nil, errAreasUnavailable
	}
	return f, nil, nil
}
