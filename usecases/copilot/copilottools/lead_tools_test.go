package copilottools

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
)

const knownLead = "7a1f0c2e-5b6d-4e8f-9a0b-1c2d3e4f5a6b"

type fakeLeads struct {
	listed lead.ListLeadsInput
	got    []string
	err    error
}

func (f *fakeLeads) List(in lead.ListLeadsInput) (*shared.PaginatedResult[*lead.LeadWithSummary], error) {
	f.listed = in
	return &shared.PaginatedResult[*lead.LeadWithSummary]{
		Items:      []*lead.LeadWithSummary{{Lead: &lead.Lead{ID: knownLead, Name: "Maria", Number: "5584994409624"}, Summary: &lead.LeadSummary{Memories: 1}}},
		Page:       in.Options.Pagination.Page,
		TotalItems: 12,
		TotalPages: 2,
	}, f.err
}

func (f *fakeLeads) Facets(lead.ListLeadsInput) (*lead.LeadFacets, error) { return nil, nil }

func (f *fakeLeads) Get(workspaceID, id string) (*lead.Lead, error) {
	f.got = append(f.got, workspaceID+"|"+id)
	if f.err != nil {
		return nil, f.err
	}
	return &lead.Lead{ID: id, Name: "Maria", Number: "5584994409624"}, nil
}

func (f *fakeLeads) GetByNumber(string, string) (*lead.Lead, error) { return nil, lead.ErrLeadNotFound }

type fakeMemories struct{ in lm.ListInput }

func (f *fakeMemories) Execute(_ context.Context, in lm.ListInput) (*lm.ListResult, error) {
	f.in = in
	return &lm.ListResult{Items: []lm.MemoryView{{LeadMemory: &lm.LeadMemory{ID: "m1", Category: lm.CategoryPreference, Content: "prefere boleto"}, ActorLabel: "Ana"}}, Total: 1}, nil
}

func leadDeps(leads *fakeLeads, memories *fakeMemories) LeadDeps {
	return LeadDeps{Leads: leads, Memories: memories}
}

func TestLeadToolsNeedLeadRead(t *testing.T) {
	deps := leadDeps(&fakeLeads{}, &fakeMemories{})
	for _, tool := range []copilot.Tool{NewSearchLeadsTool(deps), NewGetLeadTool(deps)} {
		if m := tool.Meta(); m.Resource != "leads" || m.Action != "read" || m.Mutating {
			t.Fatalf("%s meta = %+v", tool.Definition().Name, m)
		}
	}
}

func listedMaria() []*lead.LeadWithSummary {
	return []*lead.LeadWithSummary{{
		Lead: &lead.Lead{ID: knownLead, Name: "Maria", Number: "5584994409624", Addresses: []lead.Address{
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
	res := NewSearchLeadsTool(deps).Execute(context.Background(), member(), map[string]interface{}{
		"query": "maria", "has_memory": true, "page": 2, "cidade": "Natal/RN", "bairros": []interface{}{"Centro"}, "owner_id": someOwner,
	})
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %s: %s", res.Status, res.Message)
	}
	in := pages.listed
	if pages.viewer != (conversation.Viewer{UserID: "u-1", WorkspaceID: "ws-1"}) {
		t.Fatalf("viewer = %+v", pages.viewer)
	}
	if in.WorkspaceID != "ws-1" || in.Options.Pagination.Page != 2 || in.Options.Pagination.PageSize != searchPageSize {
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
		res := NewSearchLeadsTool(deps).Execute(context.Background(), member(), map[string]interface{}{"query": "maria"})
		if res.Status == copilot.StatusOK || !strings.Contains(res.Message, tc.want) {
			t.Fatalf("%s: result = %+v", name, res)
		}
	}
}

func TestSearchLeadsRefusesWithoutTheLeadPages(t *testing.T) {
	res := NewSearchLeadsTool(LeadDeps{}).Execute(context.Background(), member(), map[string]interface{}{"query": "maria"})
	if res.Status != copilot.StatusError {
		t.Fatalf("result = %+v", res)
	}
}

func TestSearchLeadsNeverListsWhenTheFilterIsRefused(t *testing.T) {
	pages := &fakePages{}
	deps, _ := filterDeps()
	deps.Pages = pages
	res := NewSearchLeadsTool(deps).Execute(context.Background(), member(), map[string]interface{}{"bairros": []interface{}{"Centro"}})
	if res.Status != copilot.StatusError || pages.listed != nil {
		t.Fatalf("result = %+v, listed %+v", res, pages.listed)
	}
}

func TestGetLeadReturnsDetailsAndMemories(t *testing.T) {
	leads, memories := &fakeLeads{}, &fakeMemories{}
	res := NewGetLeadTool(leadDeps(leads, memories)).Execute(context.Background(), member(), map[string]interface{}{"lead_id": knownLead})
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %s: %s", res.Status, res.Message)
	}
	if leads.got[0] != "ws-1|"+knownLead || memories.in.WorkspaceID != "ws-1" || memories.in.LeadID != knownLead || memories.in.Query.Limit != leadMemoryLimit {
		t.Fatalf("got %v, memories %+v", leads.got, memories.in)
	}
	b, _ := json.Marshal(res.Data)
	if !strings.Contains(string(b), "prefere boleto") || strings.Contains(string(b), "994409624") {
		t.Fatalf("data = %s", b)
	}
}

func TestGetLeadRefusesAnInventedId(t *testing.T) {
	leads := &fakeLeads{}
	res := NewGetLeadTool(leadDeps(leads, &fakeMemories{})).Execute(context.Background(), member(), map[string]interface{}{"lead_id": "maria"})
	if res.Status != copilot.StatusError || len(leads.got) != 0 {
		t.Fatalf("status %s after %v", res.Status, leads.got)
	}
}

func TestGetLeadReportsAMissingLead(t *testing.T) {
	res := NewGetLeadTool(leadDeps(&fakeLeads{err: lead.ErrLeadNotFound}, &fakeMemories{})).Execute(context.Background(), member(), map[string]interface{}{"lead_id": knownLead})
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "não encontrado") {
		t.Fatalf("result = %+v", res)
	}
}

func TestSearchConversationsOfALeadSearchesByItsNumberWithoutShowingIt(t *testing.T) {
	inbox, leads := &fakeInbox{}, &fakeLeads{}
	deps := conversationDeps(inbox, &fakeHistory{})
	deps.Leads = leads
	res := NewSearchConversationsTool(deps).Execute(context.Background(), member(), map[string]interface{}{"lead_id": knownLead})
	if res.Status != copilot.StatusOK || inbox.input.Query != "5584994409624" || leads.got[0] != "ws-1|"+knownLead {
		t.Fatalf("status %s, query %q, got %v", res.Status, inbox.input.Query, leads.got)
	}
	if res2 := NewSearchConversationsTool(deps).Execute(context.Background(), member(), map[string]interface{}{"lead_id": knownLead, "query": "x"}); res2.Status != copilot.StatusError {
		t.Fatalf("lead_id with query must be refused, got %s", res2.Status)
	}
}
