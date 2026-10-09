package lead

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/cache"
	"vozko/domain/geo"
	"vozko/domain/georef"
	workspace_domain "vozko/domain/workspace"
	geocoding_usecase "vozko/usecases/geocoding"
)

type placeCall struct {
	kind, text, state, cityCode string
}

type stubPlaces struct {
	answer geocoding_usecase.PlaceAnswer
	err    error
	calls  []placeCall
}

func (s *stubPlaces) Suggest(_ context.Context, kind, text, state, cityCode string) (geocoding_usecase.PlaceAnswer, error) {
	s.calls = append(s.calls, placeCall{kind, text, state, cityCode})
	return s.answer, s.err
}

func placeRouter(places PlaceSuggester) http.Handler {
	router := mux.NewRouter()
	handler := NewReferencePointHandler(&stubReferencePoints{})
	if places != nil {
		handler = handler.WithPlaces(places)
	}
	RegisterReferencePointRoutes(router, handler, passThrough)
	return router
}

func TestThePlaceRoutesAreGatedByFullAddresses(t *testing.T) {
	for _, path := range []string{"/leads/places/search?q=rec", "/leads/places/suggest?kind=city&q=rec"} {
		var gate gatedRoute
		router := mux.NewRouter()
		record := func(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) { gate = gatedRoute{resource, action} }
		}
		RegisterReferencePointRoutes(router, NewReferencePointHandler(nil).WithPlaces(&stubPlaces{}), record)
		send(t, router, http.MethodGet, path, nil, nil)
		if gate != (gatedRoute{workspace_domain.ResourceLeads, workspace_domain.ActionReadAddresses}) {
			t.Fatalf("%s gate = %+v, want leads:read_addresses", path, gate)
		}
	}
}

func TestPlaceSearchAnswersRankedPlacesWithBoundsAndCoverage(t *testing.T) {
	bounds := geo.BBox{South: -8.15, West: -35.0, North: -7.93, East: -34.85}
	places := &stubPlaces{answer: geocoding_usecase.PlaceAnswer{
		CoveredStates: []string{"DF", "PE"},
		Places: []georef.Place{
			{Kind: georef.PlaceCity, Key: "recife", Name: "Recife", City: "Recife", CityCode: "2611606", State: "PE", Point: geo.Point{Lat: -8.05, Lng: -34.9}, Precision: geo.PrecisionCity, Bounds: &bounds, AddressCount: 700},
			{Kind: georef.PlaceStreet, Key: "boa hora", Name: "Rua Boa Hora", Street: "Rua Boa Hora", District: "Pina", City: "Recife", CityCode: "2611606", State: "PE", ZipCode: "51030300", ZipCount: 1, Point: geo.Point{Lat: -8.09, Lng: -34.88}, Precision: geo.PrecisionStreet},
		},
	}}
	rec := send(t, placeRouter(places), http.MethodGet, "/leads/places/search?q=rec&state=PE&cityCode=2611606&kind=street", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=3600" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if len(places.calls) != 1 || places.calls[0] != (placeCall{"", "rec", "PE", "2611606"}) {
		t.Fatalf("calls = %+v, want a mixed search that ignores kind", places.calls)
	}
	var got PlaceSuggestionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 || got.Attribution != georef.Attribution || len(got.CoveredStates) != 2 {
		t.Fatalf("body = %+v", got)
	}
	city, street := got.Items[0], got.Items[1]
	if city.CityKey != "pe:recife" || city.DistrictPair != "" || street.DistrictPair != "pe:recife/pina" {
		t.Fatalf("filter keys = %q %q %q, want the lead filter keys", city.CityKey, city.DistrictPair, street.DistrictPair)
	}
	if city.Kind != "city" || city.Label != "Recife, PE" || city.Bounds == nil || city.Bounds.South != -8.15 || city.Lat != -8.05 || city.Precision != "city" {
		t.Fatalf("city = %+v", city)
	}
	if street.ZipCode != "51030300" || street.District != "Pina" || street.Label != "Rua Boa Hora, Pina, Recife, PE" || street.Bounds != nil || street.ZipCount != 1 {
		t.Fatalf("street = %+v", street)
	}
}

func TestPlaceSuggestPassesItsKind(t *testing.T) {
	places := &stubPlaces{}
	rec := send(t, placeRouter(places), http.MethodGet, "/leads/places/suggest?kind=district&q=boa&cityCode=2611606", nil, nil)
	if rec.Code != http.StatusOK || len(places.calls) != 1 || places.calls[0] != (placeCall{"district", "boa", "", "2611606"}) {
		t.Fatalf("status = %d, calls = %+v", rec.Code, places.calls)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if items, ok := body["items"].([]any); !ok || len(items) != 0 {
		t.Fatalf("items = %#v, want an empty list, never null", body["items"])
	}
}

func TestPlaceSuggestNeedsAKind(t *testing.T) {
	places := &stubPlaces{}
	rec := send(t, placeRouter(places), http.MethodGet, "/leads/places/suggest?q=boa", nil, nil)
	if rec.Code != http.StatusBadRequest || !codeIs(t, rec.Body.Bytes(), "place_query_invalid") || len(places.calls) != 0 {
		t.Fatalf("status = %d %s, calls = %+v", rec.Code, rec.Body.String(), places.calls)
	}
}

func codeIs(t *testing.T, raw []byte, code string) bool {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	return body["code"] == code
}

func TestPlaceSearchRefusals(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{georef.ErrPlaceQueryInvalid, http.StatusBadRequest, "place_query_invalid"},
		{georef.ErrPlaceScopeInvalid, http.StatusBadRequest, "place_scope_invalid"},
		{fmt.Errorf("%w: SP", geo.ErrReferenceNotLoaded), http.StatusUnprocessableEntity, "reference_not_loaded"},
		{fmt.Errorf("%w: db", geo.ErrReferenceUnavailable), http.StatusServiceUnavailable, "reference_unavailable"},
	}
	for _, tt := range tests {
		places := &stubPlaces{err: tt.err, answer: geocoding_usecase.PlaceAnswer{CoveredStates: []string{"PE"}}}
		rec := send(t, placeRouter(places), http.MethodGet, "/leads/places/search?q=sao&state=SP", nil, nil)
		if rec.Code != tt.status || !codeIs(t, rec.Body.Bytes(), tt.code) {
			t.Fatalf("%v: status = %d %s, want %d %s", tt.err, rec.Code, rec.Body.String(), tt.status, tt.code)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%v: a refusal must not be cached, Cache-Control = %q", tt.err, rec.Header().Get("Cache-Control"))
		}
		if tt.code == "reference_not_loaded" {
			var body map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &body)
			if expected, ok := body["expected"].(map[string]any); !ok || expected["coveredStates"] != "PE" {
				t.Fatalf("not loaded body = %s, want the covered states", rec.Body.String())
			}
		}
	}
}

func TestPlaceSearchBusyAsksToRetry(t *testing.T) {
	rec := send(t, placeRouter(&stubPlaces{err: cache.ErrGateBusy}), http.MethodGet, "/leads/places/search?q=rec", nil, nil)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("status = %d, Retry-After = %q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestPlaceSearchUnwiredRefuses(t *testing.T) {
	rec := send(t, placeRouter(nil), http.MethodGet, "/leads/places/search?q=rec", nil, nil)
	if rec.Code != http.StatusServiceUnavailable || !codeIs(t, rec.Body.Bytes(), "place_search_unavailable") {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
}

func TestPlaceSearchFailureIsAServerError(t *testing.T) {
	rec := send(t, placeRouter(&stubPlaces{err: fmt.Errorf("boom")}), http.MethodGet, "/leads/places/search?q=rec", nil, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
}
