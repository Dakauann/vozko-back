package leadtools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vozko/domain/address"
	"vozko/domain/cache"
	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	lm "vozko/domain/lead_memory"
	"vozko/domain/shared"
	"vozko/usecases/copilot/copilottools"
	"vozko/usecases/copilot/copilottools/tooltest"
)

type fakeMemories struct{ in lm.ListInput }

func (f *fakeMemories) Execute(_ context.Context, in lm.ListInput) (*lm.ListResult, error) {
	f.in = in
	return &lm.ListResult{Items: []lm.MemoryView{{LeadMemory: &lm.LeadMemory{ID: "m1", Category: lm.CategoryPreference, Content: "prefere boleto"}, ActorLabel: "Ana"}}, Total: 1}, nil
}

func leadDeps(leads *tooltest.FakeLeads, memories *fakeMemories) LeadDeps {
	return LeadDeps{Leads: leads, Memories: memories}
}

func TestLeadToolsNeedLeadRead(t *testing.T) {
	deps := leadDeps(&tooltest.FakeLeads{}, &fakeMemories{})
	for _, tool := range []copilot.Tool{NewSearchLeadsTool(deps), NewGetLeadTool(deps)} {
		if m := tool.Meta(); m.Resource != "leads" || m.Action != "read" || m.Mutating {
			t.Fatalf("%s meta = %+v", tool.Definition().Name, m)
		}
	}
}

func listedMaria() []*lead.LeadWithSummary {
	return []*lead.LeadWithSummary{{
		Lead: &lead.Lead{ID: tooltest.KnownLead, Name: "Maria", Number: "5584994409624", Addresses: []lead.Address{
			{Primary: true, Postal: address.Postal{Street: "Rua das Flores", Number: "123", District: "Centro", City: "Natal", State: "RN"}},
		}},
		Summary:   &lead.LeadSummary{Memories: 1},
		OwnerName: "Ana",
	}}
}

func TestSearchLeadsListsWhatTheUserMaySeeByRecentActivity(t *testing.T) {
	pages := &fakePages{items: listedMaria()}
	deps, _ := filterDeps()
	deps.Pages = pages
	res := NewSearchLeadsTool(deps).Execute(context.Background(), tooltest.Member(), map[string]interface{}{
		"query": "maria", "has_memory": true, "page": 2, "cidade": "Natal/RN", "bairros": []interface{}{"Centro"}, "owner_id": someOwner,
	})
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %s: %s", res.Status, res.Message)
	}
	in := pages.listed
	if pages.viewer != (conversation.Viewer{UserID: "u-1", WorkspaceID: "ws-1"}) {
		t.Fatalf("viewer = %+v", pages.viewer)
	}
	if in.WorkspaceID != "ws-1" || in.Options.Pagination.Page != 2 || in.Options.Pagination.PageSize != copilottools.SearchPageSize {
		t.Fatalf("input = %+v", in)
	}
	if len(in.Options.Sorts) != 1 || in.Options.Sorts[0].Field != string(lead.SortLastActivityAt) || in.Options.Sorts[0].Direction != shared.SortDesc {
		t.Fatalf("sorts = %+v", in.Options.Sorts)
	}
	fields := in.Filter.Fields()
	if len(fields) != 4 || fields[0] != crmfilter.FieldQuery || fields[1] != crmfilter.FieldMemoryCategory || fields[2] != crmfilter.FieldDistrict || fields[3] != crmfilter.FieldOwner {
		t.Fatalf("filter = %+v", in.Filter)
	}
	b, _ := json.Marshal(res.Data)
	for _, want := range []string{`"has_more":false`, `"district":"Centro"`, `"city":"Natal"`, `"owner":"Ana"`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("data misses %s: %s", want, b)
		}
	}
	for _, leaked := range []string{"994409624", "Rua das Flores"} {
		if strings.Contains(string(b), leaked) {
			t.Fatalf("data leaks %q: %s", leaked, b)
		}
	}
}

func TestSearchLeadsExplainsWhyAListIsRefused(t *testing.T) {
	cases := map[string]struct {
		err  error
		want string
	}{
		"area without addresses": {lead.ErrLeadFilterAddressForbidden, "endereços"},
		"sensitive field":        {customfield.ErrFilterSensitive, "sensível"},
		"busy analytics":         {cache.ErrGateBusy, "ocupad"},
		"no lead read":           {lead.ErrLeadForbidden, "permissão"},
	}
	for name, tc := range cases {
		deps, _ := filterDeps()
		deps.Pages = &fakePages{err: tc.err}
		res := NewSearchLeadsTool(deps).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"query": "maria"})
		if res.Status == copilot.StatusOK || !strings.Contains(res.Message, tc.want) {
			t.Fatalf("%s: result = %+v", name, res)
		}
	}
}

func TestSearchLeadsRefusesWithoutTheLeadPages(t *testing.T) {
	res := NewSearchLeadsTool(LeadDeps{}).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"query": "maria"})
	if res.Status != copilot.StatusError {
		t.Fatalf("result = %+v", res)
	}
}

func TestSearchLeadsNeverListsWhenTheFilterIsRefused(t *testing.T) {
	pages := &fakePages{}
	deps, _ := filterDeps()
	deps.Pages = pages
	res := NewSearchLeadsTool(deps).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"bairros": []interface{}{"Centro"}})
	if res.Status != copilot.StatusError || pages.listed != nil {
		t.Fatalf("result = %+v, listed %+v", res, pages.listed)
	}
}

func TestGetLeadReturnsDetailsAndMemories(t *testing.T) {
	leads, memories := &tooltest.FakeLeads{}, &fakeMemories{}
	res := NewGetLeadTool(leadDeps(leads, memories)).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"lead_id": tooltest.KnownLead})
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %s: %s", res.Status, res.Message)
	}
	if leads.Got[0] != "ws-1|"+tooltest.KnownLead || memories.in.WorkspaceID != "ws-1" || memories.in.LeadID != tooltest.KnownLead || memories.in.Query.Limit != leadMemoryLimit {
		t.Fatalf("got %v, memories %+v", leads.Got, memories.in)
	}
	b, _ := json.Marshal(res.Data)
	if !strings.Contains(string(b), "prefere boleto") || strings.Contains(string(b), "994409624") {
		t.Fatalf("data = %s", b)
	}
}

func TestGetLeadRefusesAnInventedId(t *testing.T) {
	leads := &tooltest.FakeLeads{}
	res := NewGetLeadTool(leadDeps(leads, &fakeMemories{})).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"lead_id": "maria"})
	if res.Status != copilot.StatusError || len(leads.Got) != 0 {
		t.Fatalf("status %s after %v", res.Status, leads.Got)
	}
}

func TestGetLeadReportsAMissingLead(t *testing.T) {
	res := NewGetLeadTool(leadDeps(&tooltest.FakeLeads{Err: lead.ErrLeadNotFound}, &fakeMemories{})).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"lead_id": tooltest.KnownLead})
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "não encontrado") {
		t.Fatalf("result = %+v", res)
	}
}
