package lead

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/geo"
	leaddomain "vozko/domain/lead"
	workspace_domain "vozko/domain/workspace"
	lead_usecase "vozko/usecases/lead"
)

const (
	routeMessageID = "8b3c4d5e-6f70-4182-8c1d-2e3f4a5b6c7d"
	routeAddressID = "3d4e5f60-7182-4394-a5b6-c7d8e9f00112"
)

type stubLocations struct {
	err       error
	result    *leaddomain.Lead
	calls     []string
	point     geo.Point
	leadID    string
	addressID string
	messageID string
	actor     lead_usecase.Actor
}

func (s *stubLocations) PinLocation(_ context.Context, a lead_usecase.Actor, leadID, addressID string, point geo.Point) (*leaddomain.Lead, error) {
	s.calls, s.actor, s.leadID, s.addressID, s.point = append(s.calls, "pin"), a, leadID, addressID, point
	return s.result, s.err
}

func (s *stubLocations) AcceptLocation(_ context.Context, a lead_usecase.Actor, leadID, messageID string) (*leaddomain.Lead, error) {
	s.calls, s.actor, s.leadID, s.messageID = append(s.calls, "accept"), a, leadID, messageID
	return s.result, s.err
}

func locationsHandler(locations Locations) http.Handler {
	h := NewLeadHandler(HandlerDeps{Commands: &stubCommands{}, History: &stubHistory{}, Locations: locations})
	router := mux.NewRouter()
	RegisterRoutes(router, h, passThrough)
	return router
}

func pinnedLead() *leaddomain.Lead {
	fix := geo.Fix{Point: geo.Point{Lat: -23.55052, Lng: -46.633308}, Precision: geo.PrecisionExact, Source: geo.SourceLeadPin}
	return &leaddomain.Lead{ID: routeLeadID, Version: 5, Addresses: []leaddomain.Address{{ID: routeAddressID, Label: leaddomain.AddressHome, Primary: true, Fix: &fix, GeoStatus: leaddomain.GeoLocated}}}
}

func TestAcceptLocationAnswersTheRecordWithThePin(t *testing.T) {
	locations := &stubLocations{result: pinnedLead()}
	rec := send(t, locationsHandler(locations), http.MethodPost, "/leads/"+routeLeadID+"/location-candidates/"+routeMessageID+"/accept", nil, nil)
	if rec.Code != http.StatusOK || len(locations.calls) != 1 || locations.leadID != routeLeadID || locations.messageID != routeMessageID {
		t.Fatalf("status = %d calls = %v body %s", rec.Code, locations.calls, rec.Body.String())
	}
	if locations.actor.UserID != "user-1" || locations.actor.WorkspaceID != "ws-1" {
		t.Fatalf("actor = %+v", locations.actor)
	}
	var out LeadRecordResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Version != 5 || len(out.Addresses) != 1 || out.Addresses[0].Latitude == nil {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestPinLocationReadsTheStrictBody(t *testing.T) {
	cases := []struct {
		name   string
		body   any
		status int
		calls  int
	}{
		{"a point", map[string]any{"latitude": -23.55052, "longitude": -46.633308}, http.StatusOK, 1},
		{"a missing longitude", map[string]any{"latitude": -23.55052}, http.StatusBadRequest, 0},
		{"an unknown key", map[string]any{"latitude": -23.5, "longitude": -46.6, "precision": "exact"}, http.StatusBadRequest, 0},
		{"no body", nil, http.StatusBadRequest, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			locations := &stubLocations{result: pinnedLead()}
			rec := send(t, locationsHandler(locations), http.MethodPost, "/leads/"+routeLeadID+"/addresses/"+routeAddressID+"/pin", nil, tc.body)
			if rec.Code != tc.status || len(locations.calls) != tc.calls {
				t.Fatalf("status = %d calls = %v body %s", rec.Code, locations.calls, rec.Body.String())
			}
			if tc.calls == 1 && (locations.addressID != routeAddressID || locations.point != (geo.Point{Lat: -23.55052, Lng: -46.633308})) {
				t.Fatalf("passed %s %+v", locations.addressID, locations.point)
			}
		})
	}
}

func TestLocationRefusalsMapToStatusAndCode(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{leaddomain.ErrLeadForbidden, http.StatusForbidden, "forbidden"},
		{leaddomain.ErrLocationNotFound, http.StatusNotFound, "lead_location_not_found"},
		{leaddomain.ErrAddressNotFound, http.StatusNotFound, "lead_address_not_found"},
		{leaddomain.ErrLeadNotFound, http.StatusNotFound, "lead_not_found"},
		{leaddomain.ErrLocationInvalid, http.StatusBadRequest, "lead_location_invalid"},
		{&leaddomain.VersionConflict{Current: pinnedLead()}, http.StatusConflict, "version_conflict"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			for _, call := range []struct{ path string }{
				{"/leads/" + routeLeadID + "/location-candidates/" + routeMessageID + "/accept"},
				{"/leads/" + routeLeadID + "/addresses/" + routeAddressID + "/pin"},
			} {
				rec := send(t, locationsHandler(&stubLocations{err: tc.err}), http.MethodPost, call.path, nil, map[string]any{"latitude": -23.5, "longitude": -46.6})
				var out struct {
					Code string `json:"code"`
				}
				_ = json.Unmarshal(rec.Body.Bytes(), &out)
				if rec.Code != tc.status || out.Code != tc.code {
					t.Fatalf("%s: status = %d code = %q, want %d %q", call.path, rec.Code, out.Code, tc.status, tc.code)
				}
			}
		})
	}
}

func TestLocationRoutesRefuseWithoutTheirUseCaseOrAWellFormedID(t *testing.T) {
	accept := "/leads/" + routeLeadID + "/location-candidates/" + routeMessageID + "/accept"
	if rec := send(t, locationsHandler(nil), http.MethodPost, accept, nil, nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("without the use case = %d", rec.Code)
	}
	locations := &stubLocations{result: pinnedLead()}
	if rec := send(t, locationsHandler(locations), http.MethodPost, "/leads/"+routeLeadID+"/location-candidates/not-a-message/accept", nil, nil); rec.Code != http.StatusNotFound || len(locations.calls) != 0 {
		t.Fatalf("a malformed message id = %d, calls %v", rec.Code, locations.calls)
	}
}

func TestTheLocationRoutesAreGatedByLeadsUpdate(t *testing.T) {
	gates := map[string]gatedRoute{}
	h := NewLeadHandler(HandlerDeps{Commands: &stubCommands{}, History: &stubHistory{}, Locations: &stubLocations{}})
	router := mux.NewRouter()
	RegisterRoutes(router, h, func(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			path, _ := mux.CurrentRoute(r).GetPathTemplate()
			gates[r.Method+" "+path] = gatedRoute{resource, action}
		}
	})
	send(t, router, http.MethodPost, "/leads/"+routeLeadID+"/location-candidates/"+routeMessageID+"/accept", nil, nil)
	send(t, router, http.MethodPost, "/leads/"+routeLeadID+"/addresses/"+routeAddressID+"/pin", nil, nil)
	update := gatedRoute{workspace_domain.ResourceLeads, workspace_domain.ActionUpdate}
	for _, route := range []string{
		"POST /leads" + leadIDPath + "/location-candidates" + messageIDPath + "/accept",
		"POST /leads" + leadIDPath + "/addresses" + addressIDPath + "/pin",
	} {
		if got, ok := gates[route]; !ok || got != update {
			t.Errorf("%s is gated by %+v (%v), want %+v", route, got, ok, update)
		}
	}
}
