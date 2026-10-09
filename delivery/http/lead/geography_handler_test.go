package lead

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/cache"
	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/geo"
	leaddomain "vozko/domain/lead"
	"vozko/domain/leadarea"
	"vozko/domain/leadmap"
	"vozko/domain/shared"
	workspace_domain "vozko/domain/workspace"
	lead_usecase "vozko/usecases/lead"
)

const geoAreaID = "11111111-1111-4111-8111-111111111111"

type stubMap struct {
	err       error
	filters   []crmfilter.Filter
	layerAsks []lead_usecase.LayerQuery
	tileAsks  []lead_usecase.TileQuery
	at        geo.Point
	placement crmfilter.GeoPlacement
	layer     leadmap.Layer
	summary   leadmap.Summary
	districts []leadmap.District
	viewport  leadmap.Viewport
	peek      lead_usecase.MapPeek
	leftOut   leadmap.LeftOut
}

func (s *stubMap) Summary(_ context.Context, _ lead_usecase.Actor, f crmfilter.Filter) (leadmap.Summary, error) {
	s.filters = append(s.filters, f)
	return s.summary, s.err
}

func (s *stubMap) Layer(_ context.Context, _ lead_usecase.Actor, f crmfilter.Filter, q lead_usecase.LayerQuery) (leadmap.Layer, error) {
	s.filters = append(s.filters, f)
	s.layerAsks = append(s.layerAsks, q)
	return s.layer, s.err
}

func (s *stubMap) Tile(_ context.Context, _ lead_usecase.Actor, f crmfilter.Filter, q lead_usecase.TileQuery) (leadmap.Layer, error) {
	s.filters = append(s.filters, f)
	s.tileAsks = append(s.tileAsks, q)
	return s.layer, s.err
}

func (s *stubMap) Districts(_ context.Context, _ lead_usecase.Actor, f crmfilter.Filter) ([]leadmap.District, error) {
	s.filters = append(s.filters, f)
	return s.districts, s.err
}

func (s *stubMap) Viewport(_ context.Context, _ lead_usecase.Actor, f crmfilter.Filter) (leadmap.Viewport, error) {
	s.filters = append(s.filters, f)
	return s.viewport, s.err
}

func (s *stubMap) PointLeads(_ context.Context, _ lead_usecase.Actor, f crmfilter.Filter, at geo.Point, placement crmfilter.GeoPlacement) (lead_usecase.MapPeek, error) {
	s.filters = append(s.filters, f)
	s.at = at
	s.placement = placement
	return s.peek, s.err
}

func (s *stubMap) LeftOut(_ context.Context, _ lead_usecase.Actor, f crmfilter.Filter) (leadmap.LeftOut, error) {
	s.filters = append(s.filters, f)
	return s.leftOut, s.err
}

type stubAreas struct {
	err     error
	area    leadarea.Area
	drafts  []leadarea.Draft
	patches []leadarea.Patch
	deleted []string
	actor   lead_usecase.Actor
}

func (s *stubAreas) Create(_ context.Context, a lead_usecase.Actor, d leadarea.Draft) (leadarea.Area, error) {
	s.actor = a
	s.drafts = append(s.drafts, d)
	return s.area, s.err
}

func (s *stubAreas) List(_ context.Context, a lead_usecase.Actor) ([]leadarea.Area, error) {
	s.actor = a
	return []leadarea.Area{s.area}, s.err
}

func (s *stubAreas) Get(_ context.Context, a lead_usecase.Actor, _ string) (leadarea.Area, error) {
	s.actor = a
	return s.area, s.err
}

func (s *stubAreas) Update(_ context.Context, a lead_usecase.Actor, _ string, p leadarea.Patch) (leadarea.Area, error) {
	s.actor = a
	s.patches = append(s.patches, p)
	return s.area, s.err
}

func (s *stubAreas) Delete(_ context.Context, a lead_usecase.Actor, id string) error {
	s.actor = a
	s.deleted = append(s.deleted, id)
	return s.err
}

func geographyRouter(m *stubMap, a *stubAreas) http.Handler {
	router := mux.NewRouter()
	RegisterGeographyRoutes(router, NewGeographyHandler(m, a, func() time.Time { return time.Unix(0, 0) }), passThrough)
	return router
}

func storedGeoArea() leadarea.Area {
	return leadarea.Area{
		ID: geoAreaID, WorkspaceID: "ws-1", OwnerID: "user-1", Visibility: shared.VisibilityShared, Name: "Raio da escola",
		Shape:     geo.Shape{Kind: geo.ShapeCircle, Center: geo.Point{Lat: -23.5614, Lng: -46.6559}, RadiusM: 1500},
		CreatedAt: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 10, 8, 12, 5, 0, 0, time.UTC),
	}
}

func TestEveryMapAndAreaRouteIsGatedByFullAddresses(t *testing.T) {
	gates := map[string]gatedRoute{}
	router := mux.NewRouter()
	record := func(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			path, _ := mux.CurrentRoute(r).GetPathTemplate()
			gates[r.Method+" "+path] = gatedRoute{resource, action}
		}
	}
	RegisterGeographyRoutes(router, NewGeographyHandler(&stubMap{}, &stubAreas{}, nil), record)
	calls := []struct{ method, path string }{
		{http.MethodGet, "/leads/map/summary"},
		{http.MethodGet, "/leads/map/layer"},
		{http.MethodGet, "/leads/map/tiles/12/1517/2323"},
		{http.MethodGet, "/leads/map/districts"},
		{http.MethodGet, "/leads/map/viewport"},
		{http.MethodGet, "/leads/map/point"},
		{http.MethodGet, "/leads/map/left-out"},
		{http.MethodGet, "/lead-areas"},
		{http.MethodPost, "/lead-areas"},
		{http.MethodGet, "/lead-areas/" + geoAreaID},
		{http.MethodPatch, "/lead-areas/" + geoAreaID},
		{http.MethodDelete, "/lead-areas/" + geoAreaID},
	}
	for _, call := range calls {
		send(t, router, call.method, call.path, nil, nil)
	}
	if len(gates) != len(calls) {
		t.Fatalf("gated %d of %d routes: %v", len(gates), len(calls), gates)
	}
	for route, gate := range gates {
		if gate != (gatedRoute{workspace_domain.ResourceLeads, workspace_domain.ActionReadAddresses}) {
			t.Errorf("%s is gated by %+v, want leads:read_addresses", route, gate)
		}
	}
}

func TestTheLayerAnswersPointsInTheFrontContract(t *testing.T) {
	m := &stubMap{layer: leadmap.Layer{Kind: leadmap.LayerPoints, Points: []leadmap.Point{{
		Position: geo.Point{Lat: -23.5614, Lng: -46.6559}, Precision: geo.PrecisionStreet, Placement: crmfilter.PlacementOnMap, People: 3,
		LeadIDs: []string{"a", "b", "c"}, Tone: customfield.ToneChart2, Value: "alto",
	}, {
		Position: geo.Point{Lat: -23.5614, Lng: -46.6559}, Precision: geo.PrecisionDistrict, Placement: crmfilter.PlacementApproximate, People: 2,
		LeadIDs: []string{"d", "e"}, Tone: customfield.ToneNeutral,
	}}}}
	filter := url.QueryEscape(`{"groups":[{"conjunction":"and","predicates":[{"field":"area","operator":"in","values":["` + geoAreaID + `"]}]}]}`)
	rec := send(t, geographyRouter(m, &stubAreas{}), http.MethodGet, "/leads/map/layer?bbox=-46.67,-23.57,-46.64,-23.55&zoom=15&colorBy=interesse&filter="+filter, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"kind": "points", "points": []any{map[string]any{
		"id": "-23.5614,-46.6559", "lat": -23.5614, "lng": -46.6559, "precision": "street", "placement": "on_map", "tone": "chart-2", "count": float64(3),
		"leadIds": []any{"a", "b", "c"},
	}, map[string]any{
		"id": "approximate:-23.5614,-46.6559", "lat": -23.5614, "lng": -46.6559, "precision": "district", "placement": "approximate", "tone": "neutral", "count": float64(2),
		"leadIds": []any{"d", "e"},
	}}}
	if got, _ := json.Marshal(raw); string(got) != mustJSON(t, want) {
		t.Fatalf("layer = %s\nwant    %s", got, mustJSON(t, want))
	}
	ask := m.layerAsks[0]
	if ask.BBox != (geo.BBox{South: -23.57, West: -46.67, North: -23.55, East: -46.64}) || ask.Zoom != 15 || ask.ColorBy != "interesse" {
		t.Fatalf("layer query = %+v, want bbox as west,south,east,north", ask)
	}
	if len(m.filters[0].Groups) != 1 || m.filters[0].Groups[0].Predicates[0].Field != crmfilter.FieldArea {
		t.Fatalf("the shared lead filter reaches the map: %+v", m.filters[0])
	}
}

func TestTheLayerAnswersCellsInTheFrontContract(t *testing.T) {
	m := &stubMap{layer: leadmap.Layer{Kind: leadmap.LayerCells, CellSize: 0.01, Cells: []leadmap.Cell{
		{IX: -4666, IY: -2356, Placement: crmfilter.PlacementOnMap, People: 12, Center: geo.Point{Lat: -23.555, Lng: -46.655}},
		{IX: -4666, IY: -2356, Placement: crmfilter.PlacementApproximate, People: 4, Center: geo.Point{Lat: -23.556, Lng: -46.656}},
	}}}
	rec := send(t, geographyRouter(m, &stubAreas{}), http.MethodGet, "/leads/map/layer?bbox=-47,-24,-46,-23&zoom=9", nil, nil)
	want := `{"cellSizeDegrees":0.01,"cells":[{"count":12,"ix":-4666,"iy":-2356,"lat":-23.555,"lng":-46.655,"placement":"on_map"},{"count":4,"ix":-4666,"iy":-2356,"lat":-23.556,"lng":-46.656,"placement":"approximate"}],"kind":"cells"}`
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if got, _ := json.Marshal(raw); rec.Code != http.StatusOK || string(got) != want {
		t.Fatalf("cells = %d %s\nwant %s", rec.Code, got, want)
	}
}

func TestTheLayerRefusesAMalformedWindowBeforeTheUseCase(t *testing.T) {
	for _, query := range []string{"", "bbox=-46.67,-23.57,-46.64&zoom=15", "bbox=a,b,c,d&zoom=15", "bbox=-46.67,-23.57,-46.64,-23.55", "bbox=-46.67,-23.57,-46.64,-23.55&zoom=x"} {
		m := &stubMap{}
		rec := send(t, geographyRouter(m, &stubAreas{}), http.MethodGet, "/leads/map/layer?"+query, nil, nil)
		if rec.Code != http.StatusBadRequest || errorCodeOf(t, rec.Body.Bytes()) != "map_window_invalid" || len(m.layerAsks) != 0 {
			t.Fatalf("%q = %d %s", query, rec.Code, rec.Body.String())
		}
	}
}

func TestATileAnswersItsPointsInTheLayerContract(t *testing.T) {
	m := &stubMap{layer: leadmap.Layer{Kind: leadmap.LayerPoints, Points: []leadmap.Point{{
		Position: geo.Point{Lat: -23.5614, Lng: -46.6559}, Precision: geo.PrecisionStreet, Placement: crmfilter.PlacementOnMap, People: 3,
		LeadIDs: []string{"a", "b", "c"}, Tone: customfield.ToneChart2, Value: "alto",
	}}}}
	filter := url.QueryEscape(`{"groups":[{"conjunction":"and","predicates":[{"field":"area","operator":"in","values":["` + geoAreaID + `"]}]}]}`)
	rec := send(t, geographyRouter(m, &stubAreas{}), http.MethodGet, "/leads/map/tiles/15/12136/18589?colorBy=interesse&q=maria&filter="+filter, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	want := `{"kind":"points","points":[{"count":3,"id":"-23.5614,-46.6559","lat":-23.5614,"leadIds":["a","b","c"],"lng":-46.6559,"placement":"on_map","precision":"street","tone":"chart-2"}]}`
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if got, _ := json.Marshal(raw); string(got) != want {
		t.Fatalf("tile = %s, want %s", got, want)
	}
	if ask := m.tileAsks[0]; ask != (lead_usecase.TileQuery{Z: 15, X: 12136, Y: 18589, ColorBy: "interesse"}) {
		t.Fatalf("tile query = %+v", ask)
	}
	fields := map[crmfilter.Field]bool{}
	for _, g := range m.filters[0].Groups {
		for _, p := range g.Predicates {
			fields[p.Field] = true
		}
	}
	if !fields[crmfilter.FieldArea] || !fields[crmfilter.FieldQuery] {
		t.Fatalf("the shared lead filter and the free search reach the tile: %+v", m.filters[0])
	}
}

func TestATileAnswersCellsInTheLayerContract(t *testing.T) {
	m := &stubMap{layer: leadmap.Layer{Kind: leadmap.LayerCells, CellSize: 0.01, Cells: []leadmap.Cell{
		{IX: -4666, IY: -2356, Placement: crmfilter.PlacementApproximate, People: 4, Center: geo.Point{Lat: -23.556, Lng: -46.656}},
	}}}
	rec := send(t, geographyRouter(m, &stubAreas{}), http.MethodGet, "/leads/map/tiles/9/189/290", nil, nil)
	want := `{"cellSizeDegrees":0.01,"cells":[{"count":4,"ix":-4666,"iy":-2356,"lat":-23.556,"lng":-46.656,"placement":"approximate"}],"kind":"cells"}`
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if got, _ := json.Marshal(raw); rec.Code != http.StatusOK || string(got) != want {
		t.Fatalf("cells = %d %s, want %s", rec.Code, got, want)
	}
}

func TestATileThatIsNotThreeWholeNumbersIsRefusedBeforeTheUseCase(t *testing.T) {
	for _, path := range []string{"/leads/map/tiles/x/1/2", "/leads/map/tiles/4/1.5/2", "/leads/map/tiles/4/1/%20", "/leads/map/tiles/4/1/99999999999999999999"} {
		m := &stubMap{}
		rec := send(t, geographyRouter(m, &stubAreas{}), http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusBadRequest || errorCodeOf(t, rec.Body.Bytes()) != "map_tile_invalid" || len(m.tileAsks) != 0 {
			t.Fatalf("%s = %d %s", path, rec.Code, rec.Body.String())
		}
	}
	m := &stubMap{err: fmt.Errorf("wrapped: %w", geo.ErrInvalidTile)}
	rec := send(t, geographyRouter(m, &stubAreas{}), http.MethodGet, "/leads/map/tiles/23/0/0", nil, nil)
	if rec.Code != http.StatusBadRequest || errorCodeOf(t, rec.Body.Bytes()) != "map_tile_invalid" {
		t.Fatalf("a tile the domain refuses = %d %s", rec.Code, rec.Body.String())
	}
}

func TestSummaryDistrictsAndViewportAnswerTheContract(t *testing.T) {
	m := &stubMap{
		summary:   leadmap.Summary{Total: 1200, OnMap: 800, Approximate: 200, WithoutAddress: 150, NotFound: 30, Pending: 15, QuotaExceeded: 5, Refused: 4},
		districts: []leadmap.District{{CityKey: "3550308", DistrictKey: "jardim paulista", Name: "Jardim Paulista", Position: geo.Point{Lat: -23.57, Lng: -46.66}, People: 42}},
		viewport: leadmap.Viewport{BBox: geo.BBox{South: -23.6, West: -46.7, North: -23.5, East: -46.6}, Basis: leadmap.BasisLocated, View: leadmap.ViewDistricts,
			City: &leadmap.CityPlace{CityKey: "3550308", Name: "São Paulo", State: "SP", People: 70}},
	}
	router := geographyRouter(m, &stubAreas{})
	cases := map[string]string{
		"/leads/map/summary":   `{"approximate":200,"notFound":30,"onMap":800,"pending":15,"quotaExceeded":5,"refused":4,"total":1200,"withoutAddress":150}`,
		"/leads/map/districts": `[{"cityKey":"3550308","count":42,"districtKey":"jardim paulista","lat":-23.57,"lng":-46.66,"name":"Jardim Paulista","pair":"3550308/jardim paulista"}]`,
		"/leads/map/viewport":  `{"basis":"located","bbox":{"east":-46.6,"north":-23.5,"south":-23.6,"west":-46.7},"city":{"cityKey":"3550308","count":70,"name":"São Paulo","state":"SP"},"view":"districts"}`,
	}
	for path, want := range cases {
		rec := send(t, router, http.MethodGet, path, nil, nil)
		var raw any
		_ = json.Unmarshal(rec.Body.Bytes(), &raw)
		if got, _ := json.Marshal(raw); rec.Code != http.StatusOK || string(got) != want {
			t.Fatalf("%s = %d %s\nwant %s", path, rec.Code, got, want)
		}
	}
	empty := &stubMap{districts: nil}
	rec := send(t, geographyRouter(empty, &stubAreas{}), http.MethodGet, "/leads/map/districts", nil, nil)
	if rec.Body.String() != "[]\n" {
		t.Fatalf("no bairros is an empty list, got %q", rec.Body.String())
	}
}

func TestThePeekAnswersTheLeadsAtThePosition(t *testing.T) {
	m := &stubMap{peek: lead_usecase.MapPeek{Total: 9, Leads: []*leaddomain.Lead{{ID: routeLeadID, Name: "Ana", Version: 2}}}}
	rec := send(t, geographyRouter(m, &stubAreas{}), http.MethodGet, "/leads/map/point?lat=-23.5614&lng=-46.6559", nil, nil)
	var out MapPeekResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("peek = %d %s", rec.Code, rec.Body.String())
	}
	if out.Total != 9 || len(out.Items) != 1 || out.Items[0].ID != routeLeadID || out.Items[0].Version != 2 {
		t.Fatalf("peek = %+v", out)
	}
	if m.at != (geo.Point{Lat: -23.5614, Lng: -46.6559}) || m.placement != crmfilter.PlacementOnMap {
		t.Fatalf("position = %+v %q, want the precise point when no placement is sent", m.at, m.placement)
	}
	near := &stubMap{}
	send(t, geographyRouter(near, &stubAreas{}), http.MethodGet, "/leads/map/point?lat=-6.3104&lng=-35.4793&placement=approximate", nil, nil)
	if near.placement != crmfilter.PlacementApproximate {
		t.Fatalf("placement = %q, want approximate", near.placement)
	}
	pending := &stubMap{}
	refused := send(t, geographyRouter(pending, &stubAreas{}), http.MethodGet, "/leads/map/point?lat=-6.3104&lng=-35.4793&placement=pending", nil, nil)
	if refused.Code != http.StatusBadRequest || errorCodeOf(t, refused.Body.Bytes()) != "map_placement_invalid" || len(pending.filters) != 0 {
		t.Fatalf("a pending point = %d %s", refused.Code, refused.Body.String())
	}
	bad := send(t, geographyRouter(&stubMap{}, &stubAreas{}), http.MethodGet, "/leads/map/point?lat=x&lng=-46", nil, nil)
	if bad.Code != http.StatusBadRequest || errorCodeOf(t, bad.Body.Bytes()) != "map_position_invalid" {
		t.Fatalf("bad position = %d %s", bad.Code, bad.Body.String())
	}
}

func TestMapRefusalsKeepTheirStatusAndCode(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{cache.ErrGateBusy, http.StatusServiceUnavailable, ""},
		{leaddomain.ErrLeadForbidden, http.StatusForbidden, "forbidden"},
		{leadarea.ErrAddressesRequired, http.StatusForbidden, "lead_addresses_forbidden"},
		{&customfield.FilterError{Key: "classificacao", Err: customfield.ErrFilterSensitive}, http.StatusForbidden, "custom_field_filter_sensitive_forbidden"},
		{leadmap.ErrColorByForbidden, http.StatusForbidden, "map_color_by_forbidden"},
		{leadarea.ErrNotFound, http.StatusBadRequest, "area_not_found"},
		{leadarea.ErrTooManyAreas, http.StatusBadRequest, "area_too_many"},
		{leadarea.ErrOperatorUnsupported, http.StatusBadRequest, "area_operator_unsupported"},
		{leaddomain.ErrLeadFilterInvalid, http.StatusBadRequest, "lead_filter_invalid"},
		{context.DeadlineExceeded, http.StatusGatewayTimeout, ""},
		{geo.ErrWindowTooWide, http.StatusBadRequest, "map_window_too_wide"},
		{leadmap.ErrColorByInvalid, http.StatusBadRequest, "map_color_by_invalid"},
		{errors.New("db down"), http.StatusInternalServerError, ""},
	}
	for _, tc := range cases {
		rec := send(t, geographyRouter(&stubMap{err: tc.err}, &stubAreas{}), http.MethodGet, "/leads/map/summary", nil, nil)
		if rec.Code != tc.status || (tc.code != "" && errorCodeOf(t, rec.Body.Bytes()) != tc.code) {
			t.Fatalf("%v = %d %s, want %d %s", tc.err, rec.Code, rec.Body.String(), tc.status, tc.code)
		}
		if tc.err == cache.ErrGateBusy && rec.Header().Get("Retry-After") == "" {
			t.Fatal("a busy gate tells the client when to retry")
		}
	}
	malformed := send(t, geographyRouter(&stubMap{}, &stubAreas{}), http.MethodGet, "/leads/map/summary?filter=%7Bnot-json", nil, nil)
	if malformed.Code != http.StatusBadRequest || errorCodeOf(t, malformed.Body.Bytes()) != "lead_filter_invalid" {
		t.Fatalf("malformed filter = %d %s", malformed.Code, malformed.Body.String())
	}
}

func TestCreatingAnAreaTakesTheNameFromTheClientAndEchoesTheShape(t *testing.T) {
	areas := &stubAreas{area: storedGeoArea()}
	body := map[string]any{"name": "Raio da escola", "visibility": "shared", "shape": map[string]any{"kind": "circle", "center": map[string]any{"lat": -23.5614, "lng": -46.6559}, "radiusM": 1500}}
	rec := send(t, geographyRouter(&stubMap{}, areas), http.MethodPost, "/lead-areas", nil, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	d := areas.drafts[0]
	if d.Name != "Raio da escola" || d.Visibility != shared.VisibilityShared || d.Shape.Kind != geo.ShapeCircle || d.Shape.RadiusM != 1500 || d.Shape.Center.Lat != -23.5614 {
		t.Fatalf("draft = %+v", d)
	}
	var out DrawnAreaResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != geoAreaID || !out.CanEdit || out.Shape.Kind != "circle" || out.Shape.Center == nil || out.Shape.RadiusM != 1500 || out.UpdatedAt != "2026-10-08T12:05:00Z" {
		t.Fatalf("area = %+v", out)
	}
	unknown := send(t, geographyRouter(&stubMap{}, areas), http.MethodPost, "/lead-areas", nil, map[string]any{"name": "x", "shape": map[string]any{"kind": "polygon"}, "ownerId": "someone"})
	if unknown.Code != http.StatusBadRequest || len(areas.drafts) != 1 {
		t.Fatalf("an unknown field is refused: %d", unknown.Code)
	}
}

func TestAreaRoutesMapTheirRefusals(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{leadarea.ErrNotFound, http.StatusNotFound, "area_not_found"},
		{leadarea.ErrForbidden, http.StatusForbidden, "area_forbidden"},
		{leadarea.ErrAddressesRequired, http.StatusForbidden, "lead_addresses_forbidden"},
		{leadarea.ErrNameRequired, http.StatusBadRequest, "area_name_required"},
		{leadarea.ErrShapeInvalid, http.StatusBadRequest, "area_shape_invalid"},
		{leadarea.ErrLimitReached, http.StatusBadRequest, "area_limit_reached"},
		{errors.New("db down"), http.StatusInternalServerError, ""},
	}
	for _, tc := range cases {
		areas := &stubAreas{err: tc.err, area: storedGeoArea()}
		name := "Novo"
		rec := send(t, geographyRouter(&stubMap{}, areas), http.MethodPatch, "/lead-areas/"+geoAreaID, nil, map[string]any{"name": name})
		if rec.Code != tc.status || (tc.code != "" && errorCodeOf(t, rec.Body.Bytes()) != tc.code) {
			t.Fatalf("%v = %d %s", tc.err, rec.Code, rec.Body.String())
		}
	}
	areas := &stubAreas{area: storedGeoArea()}
	router := geographyRouter(&stubMap{}, areas)
	if rec := send(t, router, http.MethodDelete, "/lead-areas/"+geoAreaID, nil, nil); rec.Code != http.StatusNoContent || areas.deleted[0] != geoAreaID {
		t.Fatalf("delete = %d", rec.Code)
	}
	rec := send(t, router, http.MethodGet, "/lead-areas", nil, nil)
	var list DrawnAreaListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Items) != 1 || list.Items[0].Name != "Raio da escola" {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}
	if areas.actor.UserID != "user-1" || areas.actor.WorkspaceID != "ws-1" {
		t.Fatalf("actor = %+v", areas.actor)
	}
	visibility := "secret"
	if rec := send(t, router, http.MethodPatch, "/lead-areas/"+geoAreaID, nil, map[string]any{"visibility": visibility}); rec.Code != http.StatusOK || *areas.patches[0].Visibility != shared.Visibility("secret") {
		t.Fatalf("the patch passes the visibility on for the domain to judge: %d", rec.Code)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func errorCodeOf(t *testing.T, body []byte) string {
	t.Helper()
	var out struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &out)
	return out.Code
}

func TestAnUnwiredMapAnswersUnavailableInsteadOfFailingOpen(t *testing.T) {
	router := mux.NewRouter()
	RegisterGeographyRoutes(router, nil, passThrough)
	for _, path := range []string{"/leads/map/summary", "/lead-areas"} {
		rec := send(t, router, http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s without a handler = %d, want 503", path, rec.Code)
		}
	}
}

func TestAnAreaBodyPastItsSizeIsRefusedBeforeItIsParsed(t *testing.T) {
	areas := &stubAreas{area: storedGeoArea()}
	ring := make([]map[string]float64, 4000)
	for i := range ring {
		ring[i] = map[string]float64{"lat": -23.5 - float64(i)*0.0001, "lng": -46.6}
	}
	body := map[string]any{"name": "Grande", "shape": map[string]any{"kind": "polygon", "ring": ring}}
	for _, method := range []string{http.MethodPost, http.MethodPatch} {
		path := "/lead-areas"
		if method == http.MethodPatch {
			path += "/" + geoAreaID
		}
		rec := send(t, geographyRouter(&stubMap{}, areas), method, path, nil, body)
		if rec.Code != http.StatusBadRequest || errorCodeOf(t, rec.Body.Bytes()) != "invalid_body" {
			t.Fatalf("%s with %d vertices = %d %s, want 400 invalid_body", method, len(ring), rec.Code, rec.Body.String())
		}
	}
	if len(areas.drafts) != 0 || len(areas.patches) != 0 {
		t.Fatal("an oversized body reaches no use case")
	}
}

func TestTheLeftOutSectionAnswersTheCountTheBairrosAndTheFilterThatListsThem(t *testing.T) {
	m := &stubMap{leftOut: leadmap.LeftOut{Total: 9, Districts: []leaddomain.DistrictCount{
		{Pair: "sp:sao paulo/centro", CityKey: "sp:sao paulo", DistrictKey: "centro", District: "Centro", City: "São Paulo", State: "SP", Count: 6},
	}}}
	structured := `{"groups":[{"conjunction":"and","predicates":[{"field":"blocked","operator":"is_false"},{"field":"area","operator":"in","values":["` + geoAreaID + `"]}]}]}`
	rec := send(t, geographyRouter(m, &stubAreas{}), http.MethodGet, "/leads/map/left-out?q=ana&filter="+url.QueryEscape(structured), nil, nil)
	var raw any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	got, _ := json.Marshal(raw)
	want := `{"districts":[{"city":"São Paulo","cityKey":"sp:sao paulo","count":6,"districtKey":"centro","name":"Centro","pair":"sp:sao paulo/centro","state":"SP"}],` +
		`"filter":{"groups":[{"conjunction":"and","predicates":[{"field":"blocked","operator":"is_false"},{"field":"area_approximate","operator":"in","values":["` + geoAreaID + `"]}]}]},"total":9}`
	if rec.Code != http.StatusOK || string(got) != want {
		t.Fatalf("left out = %d %s\nwant %s", rec.Code, got, want)
	}
	if len(m.filters) != 1 || !m.filters[0].UsesField(crmfilter.FieldQuery) || !m.filters[0].UsesField(crmfilter.FieldArea) {
		t.Fatalf("the use case reads the whole request filter, got %+v", m.filters)
	}
}

func TestTheLeftOutFilterCarriesEverySimpleParamTheCountReadButTheFreeSearch(t *testing.T) {
	m := &stubMap{leftOut: leadmap.LeftOut{Total: 2, Districts: []leaddomain.DistrictCount{}}}
	structured := `{"groups":[{"conjunction":"and","predicates":[{"field":"area","operator":"in","values":["` + geoAreaID + `"]}]}]}`
	rec := send(t, geographyRouter(m, &stubAreas{}), http.MethodGet, "/leads/map/left-out?q=bia&name=ana&blocked=false&filter="+url.QueryEscape(structured), nil, nil)
	var body struct {
		Filter crmfilter.Filter `json:"filter"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("left out = %d %s", rec.Code, rec.Body.String())
	}
	got, _ := json.Marshal(body.Filter)
	want := `{"groups":[{"conjunction":"and","predicates":[{"field":"name","operator":"contains","values":["ana"]}]},` +
		`{"conjunction":"and","predicates":[{"field":"blocked","operator":"is_false"}]},` +
		`{"conjunction":"and","predicates":[{"field":"area_approximate","operator":"in","values":["` + geoAreaID + `"]}]}]}`
	if string(got) != want {
		t.Fatalf("left out filter = %s\nwant %s", got, want)
	}
	if len(m.filters) != 1 || !m.filters[0].UsesField(crmfilter.FieldName) || !m.filters[0].UsesField(crmfilter.FieldBlocked) || !m.filters[0].UsesField(crmfilter.FieldQuery) {
		t.Fatalf("the count reads the same simple params plus the free search, got %+v", m.filters)
	}
}

func TestTheLeftOutSectionRefusesAnAreaInsideAnEitherGroupBeforeCounting(t *testing.T) {
	m := &stubMap{}
	structured := `{"groups":[{"predicates":[{"field":"area","operator":"in","values":["` + geoAreaID + `"]},{"field":"blocked","operator":"is_true"}]}]}`
	rec := send(t, geographyRouter(m, &stubAreas{}), http.MethodGet, "/leads/map/left-out?filter="+url.QueryEscape(structured), nil, nil)
	if rec.Code != http.StatusBadRequest || errorCodeOf(t, rec.Body.Bytes()) != "area_left_out_unsupported" {
		t.Fatalf("left out of an area or blocked = %d %s, want 400 area_left_out_unsupported", rec.Code, rec.Body.String())
	}
	if len(m.filters) != 0 {
		t.Fatal("a refused left out filter reaches no use case")
	}
}

func TestTheLeftOutSectionWithoutAnAreaAnswersNothingAndNoFilter(t *testing.T) {
	m := &stubMap{leftOut: leadmap.NoneLeftOut()}
	rec := send(t, geographyRouter(m, &stubAreas{}), http.MethodGet, "/leads/map/left-out", nil, nil)
	if rec.Code != http.StatusOK || rec.Body.String() != `{"total":0,"districts":[]}`+"\n" {
		t.Fatalf("left out without an area = %d %q", rec.Code, rec.Body.String())
	}
	refused := send(t, geographyRouter(&stubMap{err: leadarea.ErrNotFound}, &stubAreas{}), http.MethodGet, "/leads/map/left-out", nil, nil)
	if refused.Code != http.StatusBadRequest || errorCodeOf(t, refused.Body.Bytes()) != "area_not_found" {
		t.Fatalf("left out of a deleted area = %d %s", refused.Code, refused.Body.String())
	}
}
