package lead

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/address"
	"vozko/domain/geo"
	workspace_domain "vozko/domain/workspace"
)

type stubReferencePoints struct {
	fix     geo.ReferenceSpot
	err     error
	queries []address.Postal
}

func (s *stubReferencePoints) Locate(_ context.Context, raw address.Postal) (geo.ReferenceSpot, error) {
	s.queries = append(s.queries, raw)
	return s.fix, s.err
}

func referencePointRouter(points ReferencePointLocator) http.Handler {
	router := mux.NewRouter()
	RegisterReferencePointRoutes(router, NewReferencePointHandler(points), passThrough)
	return router
}

func TestTheReferencePointRouteIsGatedByFullAddresses(t *testing.T) {
	var gate gatedRoute
	router := mux.NewRouter()
	record := func(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { gate = gatedRoute{resource, action} }
	}
	RegisterReferencePointRoutes(router, NewReferencePointHandler(&stubReferencePoints{}), record)
	send(t, router, http.MethodGet, "/leads/map/reference-point?zipCode=01310100", nil, nil)
	if gate != (gatedRoute{workspace_domain.ResourceLeads, workspace_domain.ActionReadAddresses}) {
		t.Fatalf("gate = %+v, want leads:read_addresses", gate)
	}
}

func TestTheReferencePointAnswersThePointWithItsPrecisionAndSource(t *testing.T) {
	points := &stubReferencePoints{fix: geo.ReferenceSpot{Fix: geo.Fix{
		Point: geo.Point{Lat: -23.5614, Lng: -46.6559}, Precision: geo.PrecisionStreet, Source: geo.SourceReference, FixedAt: time.Unix(0, 0),
	}, City: "São Paulo", State: "SP"}}
	rec := send(t, referencePointRouter(points), http.MethodGet,
		"/leads/map/reference-point?zipCode=01310-100&district=Bela+Vista&city=S%C3%A3o+Paulo&state=SP", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	want := `{"attribution":"IBGE, CNEFE 2022","city":"São Paulo","lat":-23.5614,"lng":-46.6559,"precision":"street","state":"SP","wholeCity":false}`
	if got, _ := json.Marshal(raw); string(got) != want {
		t.Fatalf("body = %s\nwant   %s", got, want)
	}
	asked := address.Postal{ZipCode: "01310-100", District: "Bela Vista", City: "São Paulo", State: "SP"}
	if len(points.queries) != 1 || points.queries[0] != asked {
		t.Fatalf("queries = %+v, want %+v", points.queries, asked)
	}
}

func TestTheReferencePointSaysWhenItStartsAtTheCentreOfTheWholeCity(t *testing.T) {
	points := &stubReferencePoints{fix: geo.ReferenceSpot{Fix: geo.Fix{
		Point: geo.Point{Lat: -22.0087, Lng: -47.8909}, Precision: geo.PrecisionCity, Source: geo.SourceReference,
	}, City: "São Carlos", State: "SP", WholeCity: true}}
	rec := send(t, referencePointRouter(points), http.MethodGet, "/leads/map/reference-point?zipCode=13560-000", nil, nil)
	var body ReferencePointResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("reference point = %d %s", rec.Code, rec.Body.String())
	}
	if !body.WholeCity || body.City != "São Carlos" || body.State != "SP" || body.Precision != "city" {
		t.Fatalf("body = %+v, want the whole city of São Carlos", body)
	}
}

func TestTheReferencePointRefusesWithACode(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{geo.ErrReferenceQueryInvalid, http.StatusBadRequest, "reference_query_invalid"},
		{geo.ErrReferenceNotLoaded, http.StatusUnprocessableEntity, "reference_not_loaded"},
		{geo.ErrReferencePointNotFound, http.StatusNotFound, "reference_point_not_found"},
		{geo.ErrReferenceUnavailable, http.StatusServiceUnavailable, "reference_unavailable"},
		{errors.New("surprise"), http.StatusInternalServerError, ""},
	}
	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			rec := send(t, referencePointRouter(&stubReferencePoints{err: tt.err}), http.MethodGet, "/leads/map/reference-point?zipCode=01310100", nil, nil)
			if rec.Code != tt.status || errorCodeOf(t, rec.Body.Bytes()) != tt.code {
				t.Fatalf("answer = %d %s, want %d %q", rec.Code, rec.Body.String(), tt.status, tt.code)
			}
		})
	}
}

func TestAnUnwiredReferencePointAnswersUnavailable(t *testing.T) {
	router := mux.NewRouter()
	RegisterReferencePointRoutes(router, nil, passThrough)
	rec := send(t, router, http.MethodGet, "/leads/map/reference-point?zipCode=01310100", nil, nil)
	if rec.Code != http.StatusServiceUnavailable || errorCodeOf(t, rec.Body.Bytes()) != "reference_point_unavailable" {
		t.Fatalf("answer = %d %s, want 503 reference_point_unavailable", rec.Code, rec.Body.String())
	}
}
