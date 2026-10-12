package tooltest

import (
	"vozko/domain/lead"
	"vozko/domain/shared"
)

const KnownLead = "7a1f0c2e-5b6d-4e8f-9a0b-1c2d3e4f5a6b"

type FakeLeads struct {
	listed lead.ListLeadsInput
	Got    []string
	Err    error
}

func (f *FakeLeads) List(in lead.ListLeadsInput) (*shared.PaginatedResult[*lead.LeadWithSummary], error) {
	f.listed = in
	return &shared.PaginatedResult[*lead.LeadWithSummary]{
		Items:      []*lead.LeadWithSummary{{Lead: &lead.Lead{ID: KnownLead, Name: "Maria", Number: "5584994409624"}, Summary: &lead.LeadSummary{Memories: 1}}},
		Page:       in.Options.Pagination.Page,
		TotalItems: 12,
		TotalPages: 2,
	}, f.Err
}

func (f *FakeLeads) Facets(lead.ListLeadsInput) (*lead.LeadFacets, error) { return nil, nil }

func (f *FakeLeads) Get(workspaceID, id string) (*lead.Lead, error) {
	f.Got = append(f.Got, workspaceID+"|"+id)
	if f.Err != nil {
		return nil, f.Err
	}
	return &lead.Lead{ID: id, Name: "Maria", Number: "5584994409624"}, nil
}

func (f *FakeLeads) GetByNumber(string, string) (*lead.Lead, error) { return nil, lead.ErrLeadNotFound }
