package lead

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/cache"
	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	leaddomain "vozko/domain/lead"
	"vozko/domain/leadmap"
	workspace_domain "vozko/domain/workspace"
	lead_usecase "vozko/usecases/lead"
)

type stubSections struct {
	err    error
	asked  lead_usecase.Actor
	filter  crmfilter.Filter
	place   string
	colorBy string
}

func (s *stubSections) Summary(_ context.Context, a lead_usecase.Actor, f crmfilter.Filter) (*leaddomain.SummarySection, error) {
	s.asked, s.filter = a, f
	if s.err != nil {
		return nil, s.err
	}
	return &leaddomain.SummarySection{Total: 12, BirthdaysToday: 2}, nil
}

func (s *stubSections) FacetsColoredBy(_ context.Context, a lead_usecase.Actor, f crmfilter.Filter, colorBy string) (*leaddomain.FacetsSection, error) {
	s.asked, s.filter, s.colorBy = a, f, colorBy
	if s.err != nil {
		return nil, s.err
	}
	return &leaddomain.FacetsSection{Owners: []leaddomain.OwnerCount{{Owner: "u-1", Name: "Marina", Count: 3}}}, nil
}

func (s *stubSections) Places(_ context.Context, a lead_usecase.Actor, f crmfilter.Filter) (*leaddomain.PlacesSection, error) {
	s.asked, s.filter = a, f
	if s.err != nil {
		return nil, s.err
	}
	return &leaddomain.PlacesSection{Districts: []leaddomain.DistrictCount{{Pair: "sp:sao paulo/centro", Count: 4}}}, nil
}

func (s *stubSections) PlaceSuggestions(_ context.Context, a lead_usecase.Actor, f crmfilter.Filter, prefix string) (*leaddomain.PlacesSection, error) {
	s.asked, s.filter, s.place = a, f, prefix
	if s.err != nil {
		return nil, s.err
	}
	return &leaddomain.PlacesSection{Districts: []leaddomain.DistrictCount{{Pair: "rn:natal/santo antonio", Count: 2}}}, nil
}

func TestThePlacesSectionSuggestsPlacesForAPrefix(t *testing.T) {
	sections := &stubSections{}
	rec := send(t, sectionsHandler(sections), http.MethodGet, "/leads/sections/places?place=Santo&blocked=true", nil, nil)
	if rec.Code != http.StatusOK || sections.place != "Santo" || !sections.filter.UsesField(crmfilter.FieldBlocked) {
		t.Fatalf("suggestions = %d %s, prefix %q", rec.Code, rec.Body.String(), sections.place)
	}
	refused := send(t, sectionsHandler(&stubSections{err: fmt.Errorf("%w", leaddomain.ErrLeadSearchTooShort)}), http.MethodGet, "/leads/sections/places?place=a", nil, nil)
	if refused.Code != http.StatusBadRequest || errorCodeOf(t, refused.Body.Bytes()) != "lead_search_too_short" {
		t.Fatalf("a one letter prefix = %d %s", refused.Code, refused.Body.String())
	}
}

func TestTheFacetsSectionCountsTheFieldTheMapIsColouredBy(t *testing.T) {
	sections := &stubSections{}
	rec := send(t, sectionsHandler(sections), http.MethodGet, "/leads/sections/facets?colorBy=interesse", nil, nil)
	if rec.Code != http.StatusOK || sections.colorBy != "interesse" {
		t.Fatalf("facets = %d %s, colorBy %q", rec.Code, rec.Body.String(), sections.colorBy)
	}
	plain := &stubSections{}
	if rec := send(t, sectionsHandler(plain), http.MethodGet, "/leads/sections/facets", nil, nil); rec.Code != http.StatusOK || plain.colorBy != "" {
		t.Fatalf("plain facets = %d, colorBy %q", rec.Code, plain.colorBy)
	}
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{leadmap.ErrColorByInvalid, http.StatusBadRequest, "map_color_by_invalid"},
		{leadmap.ErrColorByForbidden, http.StatusForbidden, "map_color_by_forbidden"},
	}
	for _, c := range cases {
		refused := send(t, sectionsHandler(&stubSections{err: c.err}), http.MethodGet, "/leads/sections/facets?colorBy=x", nil, nil)
		if refused.Code != c.status || errorCodeOf(t, refused.Body.Bytes()) != c.code {
			t.Fatalf("%v = %d %s", c.err, refused.Code, refused.Body.String())
		}
	}
}

func sectionsHandler(sections Sections) http.Handler {
	h := NewLeadHandler(HandlerDeps{Commands: &stubCommands{}, History: &stubHistory{}, Sections: sections})
	router := mux.NewRouter()
	RegisterRoutes(router, h, passThrough)
	return router
}

func TestLeadSectionsAnswerEachBlockWithTheListFilter(t *testing.T) {
	sections := &stubSections{}
	filter := url.QueryEscape(`{"groups":[{"conjunction":"and","predicates":[{"field":"district","operator":"in","values":["sp:sao paulo/centro"]}]}]}`)
	rec := send(t, sectionsHandler(sections), http.MethodGet, "/leads/sections/summary?blocked=true&filter="+filter, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var out leaddomain.SummarySection
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Total != 12 || out.BirthdaysToday != 2 {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if sections.asked.UserID != "user-1" || sections.asked.WorkspaceID != "ws-1" {
		t.Fatalf("the section is read as the caller, got %+v", sections.asked)
	}
	if !sections.filter.UsesField(crmfilter.FieldBlocked) || !sections.filter.UsesField(crmfilter.FieldDistrict) {
		t.Fatalf("the section must read the same filter as the list, got %+v", sections.filter)
	}

	for _, path := range []string{"/leads/sections/facets", "/leads/sections/places"} {
		if rec := send(t, sectionsHandler(&stubSections{}), http.MethodGet, path, nil, nil); rec.Code != http.StatusOK {
			t.Fatalf("%s = %d %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestLeadSectionsRefuseWithAReason(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"busy analytics", cache.ErrGateBusy, http.StatusServiceUnavailable, ""},
		{"no permission", leaddomain.ErrLeadForbidden, http.StatusForbidden, "forbidden"},
		{"sensitive filter", &customfield.FilterError{Key: "classificacao", Err: customfield.ErrFilterSensitive}, http.StatusForbidden, "custom_field_filter_sensitive_forbidden"},
		{"invalid filter", leaddomain.ErrLeadFilterInvalid, http.StatusBadRequest, "lead_filter_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := send(t, sectionsHandler(&stubSections{err: tc.err}), http.MethodGet, "/leads/sections/facets", nil, nil)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body.String())
			}
			if tc.status == http.StatusServiceUnavailable && rec.Header().Get("Retry-After") == "" {
				t.Fatal("a busy section must say when to retry")
			}
			if tc.code == "" {
				return
			}
			var body struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != tc.code {
				t.Fatalf("code = %q, want %q", body.Code, tc.code)
			}
		})
	}
	if rec := send(t, sectionsHandler(nil), http.MethodGet, "/leads/sections/places", nil, nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("without the use case = %d", rec.Code)
	}
}

func TestLeadSectionRoutesAreGatedOnLeadsRead(t *testing.T) {
	gates := map[string]gatedRoute{}
	h := NewLeadHandler(HandlerDeps{Commands: &stubCommands{}, History: &stubHistory{}})
	router := mux.NewRouter()
	RegisterRoutes(router, h, func(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			path, _ := mux.CurrentRoute(r).GetPathTemplate()
			gates[path] = gatedRoute{resource, action}
		}
	})
	for _, section := range []string{"summary", "facets", "places"} {
		send(t, router, http.MethodGet, "/leads/sections/"+section, nil, nil)
		if got := gates["/leads/sections/"+section]; got != (gatedRoute{workspace_domain.ResourceLeads, workspace_domain.ActionRead}) {
			t.Fatalf("%s is gated by %+v", section, got)
		}
	}
}
