package copilottools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/cache"
	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/crmfilter"
	"vozko/domain/lead"
)

type fakeLeadSections struct {
	viewer  conversation.Viewer
	filters []crmfilter.Filter
	summary *lead.SummarySection
	places  *lead.PlacesSection
	err     error
}

func (f *fakeLeadSections) Summary(_ context.Context, a conversation.Viewer, filter crmfilter.Filter) (*lead.SummarySection, error) {
	f.viewer = a
	f.filters = append(f.filters, filter)
	return f.summary, f.err
}

func (f *fakeLeadSections) Places(_ context.Context, a conversation.Viewer, filter crmfilter.Filter) (*lead.PlacesSection, error) {
	f.filters = append(f.filters, filter)
	return f.places, f.err
}

func manyPlaces(n int) *lead.PlacesSection {
	places := &lead.PlacesSection{}
	for i := 0; i < n; i++ {
		places.Cities = append(places.Cities, lead.CityCount{CityKey: fmt.Sprintf("sp:cidade %d", i), City: fmt.Sprintf("Cidade %d", i), State: "SP", Count: int64(100 - i)})
		places.Districts = append(places.Districts, lead.DistrictCount{Pair: fmt.Sprintf("sp:campinas/bairro %d", i), District: fmt.Sprintf("Bairro %d", i), City: "Campinas", State: "SP", Count: int64(100 - i)})
	}
	return places
}

func TestLeadGeoSummaryCountsTheFilterAndItsBusiestPlaces(t *testing.T) {
	sections := &fakeLeadSections{
		summary: &lead.SummarySection{Total: 188000, WithAddress: 120000, OnMap: 90000, Approximate: 20000, WithoutAddress: 68000, NotFound: 3000, Refused: 450, Pending: 7000},
		places:  manyPlaces(lead.MaxPlaceCities),
	}
	deps, _ := filterDeps()
	deps.Sections = sections
	tool := NewLeadGeoSummaryTool(deps)
	if m := tool.Meta(); m.Mutating || m.Resource != "leads" || m.Action != "read" {
		t.Fatalf("meta = %+v", m)
	}
	res := tool.Execute(context.Background(), member(), map[string]interface{}{"cidade": "sp:campinas"})
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %s: %s", res.Status, res.Message)
	}
	if sections.viewer != (conversation.Viewer{UserID: "u-1", WorkspaceID: "ws-1"}) || len(sections.filters) != 2 {
		t.Fatalf("viewer %+v, filters %+v", sections.viewer, sections.filters)
	}
	for _, f := range sections.filters {
		if fields := f.Fields(); len(fields) != 1 || fields[0] != crmfilter.FieldCity {
			t.Fatalf("filter = %+v", f)
		}
	}
	b, _ := json.Marshal(res.Data)
	data := string(b)
	for _, want := range []string{`"total":188000`, `"on_map":90000`, `"without_address":68000`, `"city_key":"sp:cidade 0"`, `"pair":"sp:campinas/bairro 0"`, `"refused":450`, `"more_cities":true`, `"more_districts":true`} {
		if !strings.Contains(data, want) {
			t.Fatalf("data misses %s: %s", want, data)
		}
	}
	if strings.Contains(data, fmt.Sprintf(`"sp:cidade %d"`, geoPlacesShown)) {
		t.Fatalf("only the %d busiest places are shown: %s", geoPlacesShown, data)
	}
}

func TestLeadGeoSummaryRefusesWithoutItsSectionsOrWhenBusy(t *testing.T) {
	if res := NewLeadGeoSummaryTool(LeadDeps{}).Execute(context.Background(), member(), nil); res.Status != copilot.StatusError {
		t.Fatalf("result = %+v", res)
	}
	deps, _ := filterDeps()
	deps.Sections = &fakeLeadSections{err: cache.ErrGateBusy}
	if res := NewLeadGeoSummaryTool(deps).Execute(context.Background(), member(), nil); res.Status != copilot.StatusError || !strings.Contains(res.Message, "ocupad") {
		t.Fatalf("result = %+v", res)
	}
	deps.Sections = &fakeLeadSections{err: lead.ErrLeadForbidden}
	if res := NewLeadGeoSummaryTool(deps).Execute(context.Background(), member(), nil); res.Status != copilot.StatusDenied {
		t.Fatalf("result = %+v", res)
	}
}

func TestLeadGeoSummarySaysWhenNoPlaceWasLeftOut(t *testing.T) {
	data := geoSummaryData(&lead.SummarySection{Total: 3}, manyPlaces(geoPlacesShown))
	if data["more_cities"] != false || data["more_districts"] != false {
		t.Fatalf("data = %+v", data)
	}
}
