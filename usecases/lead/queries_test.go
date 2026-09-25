package lead_usecase

import (
	"errors"
	"testing"

	"vozko/domain/lead"
	"vozko/domain/shared"
)

type repoStub struct {
	lead.Repository
	listed   lead.ListLeadsInput
	found    *lead.Lead
	lookups  int
	faceted  bool
	numbered string
}

func (r *repoStub) ListWithSummary(in lead.ListLeadsInput) (*shared.PaginatedResult[*lead.LeadWithSummary], error) {
	r.listed = in
	return &shared.PaginatedResult[*lead.LeadWithSummary]{}, nil
}

func (r *repoStub) Facets(in lead.ListLeadsInput) (*lead.LeadFacets, error) {
	r.listed, r.faceted = in, true
	return &lead.LeadFacets{}, nil
}

func (r *repoStub) FindByID(workspaceID, id string) (*lead.Lead, error) {
	r.lookups++
	return r.found, nil
}

func (r *repoStub) FindByNumber(workspaceID, number string) (*lead.Lead, error) {
	r.lookups++
	r.numbered = number
	return r.found, nil
}

func TestQueriesRefuseToRunWithoutAWorkspace(t *testing.T) {
	repo := &repoStub{found: &lead.Lead{ID: "l1"}}
	q := NewQueries(repo)
	if _, err := q.List(lead.ListLeadsInput{}); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Fatalf("list err = %v", err)
	}
	if _, err := q.Facets(lead.ListLeadsInput{}); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Fatalf("facets err = %v", err)
	}
	if _, err := q.Get("", "l1"); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Fatalf("get err = %v", err)
	}
	if _, err := q.GetByNumber(" ", "5584"); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Fatalf("by number err = %v", err)
	}
	if repo.lookups != 0 || repo.faceted {
		t.Fatal("the repository was reached without a workspace")
	}
}

func TestListCapsThePageSize(t *testing.T) {
	repo := &repoStub{}
	in := lead.ListLeadsInput{WorkspaceID: "ws1"}
	in.Options.Pagination.PageSize = 5000
	if _, err := NewQueries(repo).List(in); err != nil {
		t.Fatal(err)
	}
	if repo.listed.Options.Pagination.PageSize != lead.MaxListPageSize {
		t.Fatalf("page size = %d", repo.listed.Options.Pagination.PageSize)
	}
}

func TestGetReportsAMissingLeadAsNotFound(t *testing.T) {
	q := NewQueries(&repoStub{})
	if _, err := q.Get("ws1", "l1"); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("get err = %v", err)
	}
	if _, err := q.GetByNumber("ws1", "5584"); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("by number err = %v", err)
	}
}

func TestGetByNumberRequiresANumber(t *testing.T) {
	repo := &repoStub{found: &lead.Lead{ID: "l1"}}
	if _, err := NewQueries(repo).GetByNumber("ws1", "  "); !errors.Is(err, lead.ErrLeadRequired) {
		t.Fatalf("err = %v", err)
	}
	if repo.lookups != 0 {
		t.Fatal("looked up an empty number")
	}
}
