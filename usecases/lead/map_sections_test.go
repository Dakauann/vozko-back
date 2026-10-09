package lead_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/cache"
	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/geo"
	"vozko/domain/lead"
	"vozko/domain/leadarea"
	"vozko/domain/leadmap"
)

const mapAreaID = "11111111-1111-4111-8111-111111111111"

type mapReader struct {
	scopes     []leadmap.Scope
	limits     []int
	placements []crmfilter.GeoPlacement
	requests   []leadmap.LayerRequest
	points     []leadmap.Point
	cells      []leadmap.Cell
	districts  []leadmap.District
	summary    leadmap.Summary
	extent     *geo.BBox
	city       *leadmap.CityPlace
	atIDs      []string
	atTotal    int
	leftOut    leadmap.LeftOut
	failWith   error
}

func (r *mapReader) record(s leadmap.Scope) error {
	r.scopes = append(r.scopes, s)
	return r.failWith
}

func (r *mapReader) Summary(_ context.Context, s leadmap.Scope) (leadmap.Summary, error) {
	return r.summary, r.record(s)
}

func (r *mapReader) Layer(_ context.Context, s leadmap.Scope, q leadmap.LayerRequest) (leadmap.Layer, error) {
	r.requests = append(r.requests, q)
	if err := r.record(s); err != nil {
		return leadmap.Layer{}, err
	}
	if len(r.points) > q.MaxPoints {
		return leadmap.Layer{Kind: leadmap.LayerCells, CellSize: q.CellSize, Cells: r.cells}, nil
	}
	return leadmap.Layer{Kind: leadmap.LayerPoints, Points: append([]leadmap.Point(nil), r.points...)}, nil
}

func (r *mapReader) Districts(_ context.Context, s leadmap.Scope, limit int) ([]leadmap.District, error) {
	r.limits = append(r.limits, limit)
	return r.districts, r.record(s)
}

func (r *mapReader) Extent(_ context.Context, s leadmap.Scope) (*geo.BBox, error) {
	return r.extent, r.record(s)
}

func (r *mapReader) TopCity(_ context.Context, s leadmap.Scope) (*leadmap.CityPlace, error) {
	return r.city, r.record(s)
}

func (r *mapReader) LeadsAt(_ context.Context, s leadmap.Scope, _ geo.Point, placement crmfilter.GeoPlacement, limit int) ([]string, int, error) {
	r.limits = append(r.limits, limit)
	r.placements = append(r.placements, placement)
	return r.atIDs, r.atTotal, r.record(s)
}

func (r *mapReader) LeftOut(_ context.Context, s leadmap.Scope, limit int) (leadmap.LeftOut, error) {
	r.limits = append(r.limits, limit)
	return r.leftOut, r.record(s)
}

type mapAreas struct {
	updated time.Time
	err     error
	calls   int
	shape   geo.Shape
}

func (m *mapAreas) BindAreas(_ context.Context, _ Actor, f crmfilter.Filter) (crmfilter.Filter, leadarea.Stamps, error) {
	m.calls++
	if m.err != nil {
		return crmfilter.Filter{}, nil, m.err
	}
	ids, err := leadarea.IDsIn(f)
	if err != nil || len(ids) == 0 {
		return f, nil, err
	}
	found := []leadarea.Area{{ID: mapAreaID, WorkspaceID: cmdWorkspace, OwnerID: cmdUser, Visibility: "private", UpdatedAt: m.updated, Shape: m.shape}}
	return leadarea.Bind(f, cmdWorkspace, cmdUser, found)
}

type mapLeads struct {
	leads []*lead.Lead
	asked []string
}

func (m *mapLeads) FindByIDs(_ string, ids []string) ([]*lead.Lead, error) {
	m.asked = ids
	return m.leads, nil
}

var mapViewer = fakePermissions{"leads:read": true, "leads:read_addresses": true}

func classificationDefinitions() []*customfield.Definition {
	defs := leadDefinitions()
	for _, d := range defs {
		if d.Key == "interesse" {
			d.OptionTones = map[string]customfield.Tone{"alto": customfield.ToneChart2}
		}
	}
	return defs
}

type mapFixture struct {
	reader   *mapReader
	areas    *mapAreas
	leads    *mapLeads
	contacts *pageContacts
	memo     *sectionMemo
	gate     *sectionGate
	versions *sectionVersions
}

func newMapFixture() *mapFixture {
	return &mapFixture{
		reader: &mapReader{}, areas: &mapAreas{updated: time.Unix(100, 0)}, leads: &mapLeads{}, contacts: &pageContacts{},
		memo: &sectionMemo{values: map[string][]byte{}}, gate: &sectionGate{}, versions: &sectionVersions{generation: 3},
	}
}

func (f *mapFixture) sections(t *testing.T, perms fakePermissions) *MapSections {
	t.Helper()
	s, err := NewMapSections(MapDeps{
		Reader: f.reader, Areas: f.areas, Leads: f.leads, Contacts: f.contacts, Permissions: perms,
		Definitions: &fakeDefinitions{defs: classificationDefinitions()}, Zones: fixedZones{loc: time.UTC},
		Caching: SectionCaching{Memo: f.memo, Gate: f.gate, Versions: f.versions, TTL: time.Minute},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func streetLayer() LayerQuery {
	return LayerQuery{BBox: geo.BBox{South: -23.57, West: -46.67, North: -23.55, East: -46.64}, Zoom: 15}
}

func streetTile() TileQuery {
	return TileQuery{Z: 15, X: 12136, Y: 18589}
}

func areaOnly() crmfilter.Filter {
	return crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldArea, Operator: crmfilter.OpIn, Values: []string{mapAreaID}},
	}}}}
}

func TestMapSectionsRefuseAMissingDependency(t *testing.T) {
	f := newMapFixture()
	full := MapDeps{
		Reader: f.reader, Areas: f.areas, Leads: f.leads, Contacts: f.contacts, Permissions: mapViewer,
		Definitions: &fakeDefinitions{}, Zones: fixedZones{loc: time.UTC},
		Caching: SectionCaching{Memo: f.memo, Gate: f.gate, Versions: f.versions},
	}
	cases := map[string]func(d *MapDeps){
		"reader":   func(d *MapDeps) { d.Reader = nil },
		"areas":    func(d *MapDeps) { d.Areas = nil },
		"leads":    func(d *MapDeps) { d.Leads = nil },
		"contacts": func(d *MapDeps) { d.Contacts = nil },
		"memo":     func(d *MapDeps) { d.Caching.Memo = nil },
		"gate":     func(d *MapDeps) { d.Caching.Gate = nil },
		"versions": func(d *MapDeps) { d.Caching.Versions = nil },
	}
	for name, drop := range cases {
		deps := full
		drop(&deps)
		if _, err := NewMapSections(deps); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("missing %s = %v", name, err)
		}
	}
}

func TestEveryMapSectionNeedsLeadsAndFullAddresses(t *testing.T) {
	ctx := context.Background()
	for name, perms := range map[string]fakePermissions{
		"no address permission":   {"leads:read": true},
		"addresses without leads": {"leads:read_addresses": true},
	} {
		f := newMapFixture()
		s := f.sections(t, perms)
		if _, err := s.Summary(ctx, operator(), crmfilter.Filter{}); !errors.Is(err, lead.ErrLeadForbidden) {
			t.Fatalf("%s: summary = %v", name, err)
		}
		if _, err := s.Layer(ctx, operator(), crmfilter.Filter{}, streetLayer()); !errors.Is(err, lead.ErrLeadForbidden) {
			t.Fatalf("%s: layer = %v", name, err)
		}
		if _, err := s.Tile(ctx, operator(), crmfilter.Filter{}, streetTile()); !errors.Is(err, lead.ErrLeadForbidden) {
			t.Fatalf("%s: tile = %v", name, err)
		}
		if _, err := s.Districts(ctx, operator(), crmfilter.Filter{}); !errors.Is(err, lead.ErrLeadForbidden) {
			t.Fatalf("%s: districts = %v", name, err)
		}
		if _, err := s.Viewport(ctx, operator(), crmfilter.Filter{}); !errors.Is(err, lead.ErrLeadForbidden) {
			t.Fatalf("%s: viewport = %v", name, err)
		}
		if _, err := s.PointLeads(ctx, operator(), crmfilter.Filter{}, geo.Point{Lat: -23.5, Lng: -46.6}, crmfilter.PlacementOnMap); !errors.Is(err, lead.ErrLeadForbidden) {
			t.Fatalf("%s: peek = %v", name, err)
		}
		if len(f.reader.scopes) != 0 {
			t.Fatalf("%s: a refused section reads nothing", name)
		}
	}
}

func TestTheLayerDrawsPointsUpToFiveThousandPositionsWithTheirTones(t *testing.T) {
	f := newMapFixture()
	f.reader.points = []leadmap.Point{
		{Position: geo.Point{Lat: -23.56, Lng: -46.65}, Precision: geo.PrecisionStreet, People: 3, LeadIDs: []string{"a", "b", "c"}, Value: "alto"},
		{Position: geo.Point{Lat: -23.55, Lng: -46.64}, Precision: geo.PrecisionExact, People: 1, LeadIDs: []string{"d"}, Value: "baixo"},
	}
	q := streetLayer()
	q.ColorBy = "interesse"
	layer, err := f.sections(t, mapViewer).Layer(context.Background(), operator(), crmfilter.Filter{}, q)
	if err != nil {
		t.Fatal(err)
	}
	if layer.Kind != leadmap.LayerPoints || len(layer.Points) != 2 {
		t.Fatalf("layer = %+v", layer)
	}
	if layer.Points[0].Tone != customfield.ToneChart2 || layer.Points[1].Tone != customfield.ToneNeutral {
		t.Fatalf("tones = %q %q, want the option tone and neutral for an option without one", layer.Points[0].Tone, layer.Points[1].Tone)
	}
	if q := f.reader.requests[0]; q.MaxPoints != leadmap.MaxPoints || q.ColorByKey != "interesse" || q.CellSize != snapped(t, streetLayer()).CellSize(geo.MaxCellsPerAxis) {
		t.Fatalf("the reader is asked with the 5,000 switch, the colour-by and the zoom cell size: %+v", q)
	}
	if f.gate.acquired != 1 {
		t.Fatalf("the layer runs behind the gate, acquired %d", f.gate.acquired)
	}
}

func TestTheLayerBecomesCellsPastFiveThousandPositions(t *testing.T) {
	f := newMapFixture()
	f.reader.points = make([]leadmap.Point, leadmap.MaxPoints+1)
	f.reader.cells = []leadmap.Cell{{IX: -1, IY: -2, People: 9000, Center: geo.Point{Lat: -23.5, Lng: -46.6}}}
	q := LayerQuery{BBox: geo.BBox{South: -24, West: -47, North: -23, East: -46}, Zoom: 10}
	layer, err := f.sections(t, mapViewer).Layer(context.Background(), operator(), crmfilter.Filter{}, q)
	if err != nil {
		t.Fatal(err)
	}
	if layer.Kind != leadmap.LayerCells || len(layer.Cells) != 1 || len(layer.Points) != 0 {
		t.Fatalf("layer = %+v", layer)
	}
	if layer.CellSize != snapped(t, q).CellSize(geo.MaxCellsPerAxis) || f.reader.requests[0].Claim != snapped(t, q).Claim() {
		t.Fatalf("cell size = %v, want the window's bounded cell size", layer.CellSize)
	}
}

func TestTheLayerRefusesWhatCannotBeDrawnBeforeReading(t *testing.T) {
	cases := []struct {
		name string
		q    LayerQuery
		want error
	}{
		{"an inverted box", LayerQuery{BBox: geo.BBox{South: -23, West: -46, North: -24, East: -45}, Zoom: 10}, geo.ErrInvalidBBox},
		{"no zoom past the tiles", LayerQuery{BBox: geo.BBox{South: -24, West: -47, North: -23, East: -46}, Zoom: 30}, geo.ErrInvalidWindow},
		{"the country at street zoom", LayerQuery{BBox: geo.BrazilBounds(), Zoom: 16}, geo.ErrWindowTooWide},
		{"an unknown colour-by", LayerQuery{BBox: geo.BBox{South: -24, West: -47, North: -23, East: -46}, Zoom: 10, ColorBy: "partido"}, leadmap.ErrColorByInvalid},
		{"a sensitive colour-by", LayerQuery{BBox: geo.BBox{South: -24, West: -47, North: -23, East: -46}, Zoom: 10, ColorBy: "classificacao"}, leadmap.ErrColorByForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newMapFixture()
			if _, err := f.sections(t, mapViewer).Layer(context.Background(), operator(), crmfilter.Filter{}, tc.q); !errors.Is(err, tc.want) {
				t.Fatalf("Layer() = %v, want %v", err, tc.want)
			}
			if len(f.reader.scopes) != 0 {
				t.Fatal("a refused layer reads nothing")
			}
		})
	}
}

func TestAnAreaIsResolvedBeforeEverySectionAndADeletedOneRefuses(t *testing.T) {
	ctx := context.Background()
	f := newMapFixture()
	s := f.sections(t, mapViewer)
	if _, err := s.Summary(ctx, operator(), areaOnly()); err != nil {
		t.Fatal(err)
	}
	if bound := f.reader.scopes[0].Filter.Groups[0].Predicates[0]; bound.Field != crmfilter.FieldArea {
		t.Fatalf("scope filter = %+v", f.reader.scopes[0].Filter)
	}
	if _, ok := f.reader.scopes[0].Filter.Groups[0].Predicates[0].BoundKind(); !ok {
		t.Fatal("the reader must receive the bound area predicate")
	}

	f.areas.err = leadarea.ErrNotFound
	calls := len(f.reader.scopes)
	for name, run := range map[string]func() error{
		"summary":   func() error { _, err := s.Summary(ctx, operator(), areaOnly()); return err },
		"layer":     func() error { _, err := s.Layer(ctx, operator(), areaOnly(), streetLayer()); return err },
		"tile":      func() error { _, err := s.Tile(ctx, operator(), areaOnly(), streetTile()); return err },
		"districts": func() error { _, err := s.Districts(ctx, operator(), areaOnly()); return err },
		"viewport":  func() error { _, err := s.Viewport(ctx, operator(), areaOnly()); return err },
		"peek": func() error {
			_, err := s.PointLeads(ctx, operator(), areaOnly(), geo.Point{Lat: -23.5, Lng: -46.6}, crmfilter.PlacementOnMap)
			return err
		},
	} {
		if err := run(); !errors.Is(err, leadarea.ErrNotFound) {
			t.Fatalf("%s with a deleted area = %v, want ErrNotFound", name, err)
		}
	}
	if len(f.reader.scopes) != calls {
		t.Fatal("a deleted area reads nothing")
	}
}

func TestTheCacheKeyFollowsTheGenerationTheAreaEditAndTheTiles(t *testing.T) {
	ctx := context.Background()
	f := newMapFixture()
	s := f.sections(t, mapViewer)
	run := func(q LayerQuery) {
		t.Helper()
		if _, err := s.Layer(ctx, operator(), areaOnly(), q); err != nil {
			t.Fatal(err)
		}
	}
	run(streetLayer())
	run(streetLayer())
	if len(f.reader.scopes) != 1 {
		t.Fatalf("the same window twice reads once, read %d", len(f.reader.scopes))
	}
	moved := streetLayer()
	moved.BBox.West, moved.BBox.East = -46.70, -46.68
	run(moved)
	f.areas.updated = time.Unix(200, 0)
	run(streetLayer())
	f.versions.generation++
	run(streetLayer())
	if len(f.reader.scopes) != 4 {
		t.Fatalf("other tiles, a reshaped area and a new generation each read again: %d reads", len(f.reader.scopes))
	}
	for _, key := range f.memo.keys {
		if !strings.HasPrefix(key, "leads:map:"+cmdWorkspace+":") {
			t.Fatalf("key %q is not scoped to the workspace", key)
		}
	}
}

func TestABusyGateAnswersBusy(t *testing.T) {
	f := newMapFixture()
	f.gate.busy = true
	if _, err := f.sections(t, mapViewer).Summary(context.Background(), operator(), crmfilter.Filter{}); !errors.Is(err, cache.ErrGateBusy) {
		t.Fatalf("summary = %v, want ErrGateBusy", err)
	}
}

func TestDistrictsKeepOnlyBairrosThatCanBeDrawn(t *testing.T) {
	f := newMapFixture()
	f.reader.districts = []leadmap.District{
		{CityKey: "3550308", DistrictKey: "bela vista", Name: "Bela Vista", Position: geo.Point{Lat: -23.56, Lng: -46.65}, People: 4},
		{CityKey: "3550308", DistrictKey: "se", Name: "Sé", People: 2},
	}
	got, err := f.sections(t, mapViewer).Districts(context.Background(), operator(), crmfilter.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].DistrictKey != "bela vista" || f.reader.limits[0] != leadmap.MaxDistricts {
		t.Fatalf("districts = %+v", got)
	}
}

func TestTheViewportFollowsTheDefaultRule(t *testing.T) {
	f := newMapFixture()
	f.reader.summary = leadmap.Summary{Total: 100, OnMap: 10}
	f.reader.city = &leadmap.CityPlace{CityKey: "3550308", Name: "São Paulo", State: "SP", People: 70}
	v, err := f.sections(t, mapViewer).Viewport(context.Background(), operator(), crmfilter.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if v.Basis != leadmap.BasisCountry || v.View != leadmap.ViewDistricts || v.City == nil || v.City.Name != "São Paulo" {
		t.Fatalf("viewport = %+v", v)
	}
	f2 := newMapFixture()
	f2.reader.summary = leadmap.Summary{Total: 10, OnMap: 9}
	f2.reader.extent = &geo.BBox{South: -23.6, West: -46.7, North: -23.5, East: -46.6}
	v, err = f2.sections(t, mapViewer).Viewport(context.Background(), operator(), crmfilter.Filter{})
	if err != nil || v.Basis != leadmap.BasisLocated || v.View != leadmap.ViewPositions {
		t.Fatalf("viewport = %+v %v", v, err)
	}
}

func TestThePeekProjectsTheLeadsAtThePositionInTheirOrder(t *testing.T) {
	f := newMapFixture()
	f.reader.atIDs, f.reader.atTotal = []string{"l-2", "l-1"}, 7
	f.leads.leads = []*lead.Lead{
		{ID: "l-1", WorkspaceID: cmdWorkspace, Name: "Ana", CustomFields: map[string]any{"classificacao": "positivo", "cor": "azul"}},
		{ID: "l-2", WorkspaceID: cmdWorkspace, Name: "Bia"},
	}
	peek, err := f.sections(t, mapViewer).PointLeads(context.Background(), operator(), crmfilter.Filter{}, geo.Point{Lat: -23.5614, Lng: -46.6559}, crmfilter.PlacementApproximate)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.reader.placements) != 1 || f.reader.placements[0] != crmfilter.PlacementApproximate {
		t.Fatalf("the peek reads the placement of its point, got %v", f.reader.placements)
	}
	if peek.Total != 7 || len(peek.Leads) != 2 || peek.Leads[0].ID != "l-2" || peek.Leads[1].ID != "l-1" {
		t.Fatalf("peek = %+v", peek)
	}
	if _, sensitive := peek.Leads[1].CustomFields["classificacao"]; sensitive {
		t.Fatal("the peek is projected by VisibleFields")
	}
	if len(peek.Leads[1].Phones) != 1 || f.contacts.calls != 1 || f.reader.limits[0] != leadmap.MaxPeekLeads {
		t.Fatalf("the peek carries phones from one contact read, limit %v", f.reader.limits)
	}
	if _, err := f.sections(t, mapViewer).PointLeads(context.Background(), operator(), crmfilter.Filter{}, geo.Point{}, crmfilter.PlacementOnMap); !errors.Is(err, leadmap.ErrInvalidPosition) {
		t.Fatalf("null island = %v, want ErrInvalidPosition", err)
	}
	if _, err := f.sections(t, mapViewer).PointLeads(context.Background(), operator(), crmfilter.Filter{}, geo.Point{Lat: -23.5614, Lng: -46.6559}, crmfilter.PlacementPending); !errors.Is(err, leadmap.ErrInvalidPlacement) {
		t.Fatalf("a pending point = %v, want ErrInvalidPlacement", err)
	}
	if _, err := f.sections(t, mapViewer).PointLeads(context.Background(), operator(), crmfilter.Filter{}, geo.Point{Lat: -23.5614, Lng: -46.6559}, ""); !errors.Is(err, leadmap.ErrInvalidPlacement) {
		t.Fatalf("a point without a placement = %v, want ErrInvalidPlacement", err)
	}
	if len(f.reader.placements) != 1 {
		t.Fatalf("a refused placement never reaches the reader, got %v", f.reader.placements)
	}
}

func TestAnUnknownCustomFieldInTheMapFilterIsAnInvalidFilter(t *testing.T) {
	f := newMapFixture()
	bad := crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldCustom, Key: "partido", Operator: crmfilter.OpEquals, Values: []string{"x"}}}}}}
	if _, err := f.sections(t, mapViewer).Summary(context.Background(), operator(), bad); !errors.Is(err, lead.ErrLeadFilterInvalid) {
		t.Fatalf("summary = %v, want ErrLeadFilterInvalid", err)
	}
}

func snapped(t *testing.T, q LayerQuery) geo.Window {
	t.Helper()
	w, err := geo.SnapWindow(q.BBox, q.Zoom)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestTheLeftOutSectionReadsEveryAreaThroughApproximatePositions(t *testing.T) {
	f := newMapFixture()
	f.reader.leftOut = leadmap.LeftOut{Total: 4, Districts: []lead.DistrictCount{{Pair: "sp:sao paulo/centro", CityKey: "sp:sao paulo", DistrictKey: "centro", Count: 3}}}
	s := f.sections(t, mapViewer)
	for range 2 {
		got, err := s.LeftOut(context.Background(), operator(), areaOnly())
		if err != nil {
			t.Fatal(err)
		}
		if got.Total != 4 || len(got.Districts) != 1 || got.Districts[0].Pair != "sp:sao paulo/centro" {
			t.Fatalf("left out = %+v", got)
		}
	}
	if len(f.reader.scopes) != 1 || f.reader.limits[0] != leadmap.MaxLeftOutDistricts {
		t.Fatalf("the left out section reads once and is remembered, got %d reads with limits %v", len(f.reader.scopes), f.reader.limits)
	}
	area := f.reader.scopes[0].Filter.Groups[0].Predicates[0]
	if area.Field != crmfilter.FieldAreaApproximate || len(area.BoundAreas()) != 1 || area.BoundAreas()[0].ID != mapAreaID {
		t.Fatalf("the reader must count the area through approximate positions with its bounds, got %+v", area)
	}
	if _, err := s.Summary(context.Background(), operator(), areaOnly()); err != nil {
		t.Fatal(err)
	}
	if len(f.reader.scopes) != 2 || f.reader.scopes[1].Filter.Groups[0].Predicates[0].Field != crmfilter.FieldArea {
		t.Fatal("the summary of the same filter keeps its own cache entry and reads the pinned positions")
	}
}

func TestNothingIsLeftOutOfAFilterWithoutAnArea(t *testing.T) {
	f := newMapFixture()
	f.reader.leftOut = leadmap.LeftOut{Total: 99}
	got, err := f.sections(t, mapViewer).LeftOut(context.Background(), operator(), crmfilter.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 0 || got.Districts == nil || len(got.Districts) != 0 || len(f.reader.scopes) != 0 {
		t.Fatalf("left out without an area = %+v after %d reads, want nothing and no read", got, len(f.reader.scopes))
	}
}

func TestTheLeftOutSectionNeedsLeadsAndFullAddressesAndAReadableArea(t *testing.T) {
	f := newMapFixture()
	if _, err := f.sections(t, fakePermissions{"leads:read": true}).LeftOut(context.Background(), operator(), areaOnly()); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("left out without read_addresses = %v, want ErrLeadForbidden", err)
	}
	f.areas.err = leadarea.ErrNotFound
	if _, err := f.sections(t, mapViewer).LeftOut(context.Background(), operator(), areaOnly()); !errors.Is(err, leadarea.ErrNotFound) {
		t.Fatalf("left out of a deleted area = %v, want ErrNotFound", err)
	}
	if len(f.reader.scopes) != 0 {
		t.Fatal("a refused left out section reads nothing")
	}
}

func TestTheLeftOutSectionRefusesAnAreaThatSharesAnEitherGroupWithAnotherTest(t *testing.T) {
	f := newMapFixture()
	either := crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldArea, Operator: crmfilter.OpIn, Values: []string{mapAreaID}},
		{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsTrue},
	}}}}
	if _, err := f.sections(t, mapViewer).LeftOut(context.Background(), operator(), either); !errors.Is(err, leadarea.ErrLeftOutUnsupported) {
		t.Fatalf("left out of an area or blocked = %v, want ErrLeftOutUnsupported", err)
	}
	if len(f.reader.scopes) != 0 {
		t.Fatal("a refused left out section reads nothing")
	}
}

func TestATileDrawsPointsUpToItsCapWithTheirTones(t *testing.T) {
	f := newMapFixture()
	f.reader.points = []leadmap.Point{
		{Position: geo.Point{Lat: -23.56, Lng: -46.65}, Precision: geo.PrecisionStreet, People: 3, LeadIDs: []string{"a", "b", "c"}, Value: "alto"},
		{Position: geo.Point{Lat: -23.55, Lng: -46.64}, Precision: geo.PrecisionExact, People: 1, LeadIDs: []string{"d"}, Value: "baixo"},
	}
	q := streetTile()
	q.ColorBy = "interesse"
	layer, err := f.sections(t, mapViewer).Tile(context.Background(), operator(), crmfilter.Filter{}, q)
	if err != nil {
		t.Fatal(err)
	}
	if layer.Kind != leadmap.LayerPoints || len(layer.Points) != 2 {
		t.Fatalf("tile = %+v", layer)
	}
	if layer.Points[0].Tone != customfield.ToneChart2 || layer.Points[1].Tone != customfield.ToneNeutral {
		t.Fatalf("tones = %q %q, want the option tone and neutral for an option without one", layer.Points[0].Tone, layer.Points[1].Tone)
	}
	tile, _ := geo.TileAt(q.Z, q.X, q.Y)
	if r := f.reader.requests[0]; r.MaxPoints != leadmap.MaxTilePoints || r.ColorByKey != "interesse" || r.CellSize != tile.CellSize() || r.Claim != tile.Claim() {
		t.Fatalf("the reader is asked for the tile claim, the tile cap, its cell size and the colour-by: %+v", r)
	}
	if f.gate.acquired != 1 {
		t.Fatalf("the tile runs behind the gate, acquired %d", f.gate.acquired)
	}
}

func TestADenseTileAnswersCellsOfItsOwnGrid(t *testing.T) {
	f := newMapFixture()
	f.reader.points = make([]leadmap.Point, leadmap.MaxTilePoints+1)
	f.reader.cells = []leadmap.Cell{{IX: -1, IY: -2, People: 9000, Center: geo.Point{Lat: -23.5, Lng: -46.6}}}
	q := TileQuery{Z: 10, X: 379, Y: 580}
	layer, err := f.sections(t, mapViewer).Tile(context.Background(), operator(), crmfilter.Filter{}, q)
	if err != nil {
		t.Fatal(err)
	}
	if layer.Kind != leadmap.LayerCells || len(layer.Cells) != 1 || layer.CellSize != geo.CellSizeDegrees(10) {
		t.Fatalf("tile = %+v, want cells of the zoom grid", layer)
	}
}

func TestATileRefusesWhatCannotBeDrawnBeforeReading(t *testing.T) {
	cases := []struct {
		name string
		q    TileQuery
		want error
	}{
		{"a zoom past the deepest tile", TileQuery{Z: geo.MaxZoom + 1}, geo.ErrInvalidTile},
		{"a negative zoom", TileQuery{Z: -1}, geo.ErrInvalidTile},
		{"a column past the grid", TileQuery{Z: 4, X: 16, Y: 3}, geo.ErrInvalidTile},
		{"a negative row", TileQuery{Z: 4, X: 3, Y: -1}, geo.ErrInvalidTile},
		{"an unknown colour-by", TileQuery{Z: 10, X: 379, Y: 580, ColorBy: "partido"}, leadmap.ErrColorByInvalid},
		{"a sensitive colour-by", TileQuery{Z: 10, X: 379, Y: 580, ColorBy: "classificacao"}, leadmap.ErrColorByForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newMapFixture()
			if _, err := f.sections(t, mapViewer).Tile(context.Background(), operator(), crmfilter.Filter{}, tc.q); !errors.Is(err, tc.want) {
				t.Fatalf("Tile() = %v, want %v", err, tc.want)
			}
			if len(f.reader.scopes) != 0 {
				t.Fatal("a refused tile reads nothing")
			}
		})
	}
}

func TestEachTileIsCachedByItsOwnKeyAndReadAgainWhenTheDataTheAreaTheTierOrTheColourMoves(t *testing.T) {
	ctx := context.Background()
	f := newMapFixture()
	s := f.sections(t, mapViewer)
	run := func(s *MapSections, q TileQuery) {
		t.Helper()
		if _, err := s.Tile(ctx, operator(), areaOnly(), q); err != nil {
			t.Fatal(err)
		}
	}
	run(s, streetTile())
	run(s, streetTile())
	if len(f.reader.scopes) != 1 {
		t.Fatalf("the same tile twice reads once, read %d", len(f.reader.scopes))
	}
	next := streetTile()
	next.X++
	run(s, next)
	run(s, next)
	if len(f.reader.scopes) != 2 {
		t.Fatalf("a neighbour tile reads once on its own key, read %d", len(f.reader.scopes))
	}
	coloured := streetTile()
	coloured.ColorBy = "interesse"
	run(s, coloured)
	f.areas.updated = time.Unix(200, 0)
	run(s, streetTile())
	f.versions.generation++
	run(s, streetTile())
	sensitive := f.sections(t, fakePermissions{"leads:read": true, "leads:read_addresses": true, "leads:read_sensitive": true})
	run(sensitive, streetTile())
	if len(f.reader.scopes) != 6 {
		t.Fatalf("a colour-by, a reshaped area, a new generation and another field tier each read again: %d reads", len(f.reader.scopes))
	}
	layerKeys := len(f.memo.keys)
	if _, err := s.Layer(ctx, operator(), areaOnly(), streetLayer()); err != nil {
		t.Fatal(err)
	}
	if len(f.memo.keys) == layerKeys {
		t.Fatal("a tile never shares a cache entry with a window")
	}
	for _, key := range f.memo.keys {
		if !strings.HasPrefix(key, "leads:map:"+cmdWorkspace+":") {
			t.Fatalf("key %q is not scoped to the workspace", key)
		}
	}
}
