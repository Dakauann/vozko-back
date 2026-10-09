package lead

import (
	"strings"
	"testing"

	"vozko/domain/lead"
	"vozko/domain/shared"
	infracrmfilter "vozko/infra/repositories/crmfilter"
)

func TestOrderByAlwaysEndsWithAUniqueTiebreaker(t *testing.T) {
	cases := [][]shared.Sort{
		nil,
		{{Field: string(lead.SortName), Direction: shared.SortAsc}},
		{{Field: string(lead.SortCampaigns), Direction: shared.SortDesc}},
		{
			{Field: string(lead.SortLastActivityAt), Direction: shared.SortDesc},
			{Field: string(lead.SortName), Direction: shared.SortAsc},
		},
	}
	for _, sorts := range cases {
		if got := orderBy(testDescriptor, sorts); !strings.HasSuffix(got, ", leads.id DESC") {
			t.Errorf("orderBy(%v) = %q, want a leads.id tiebreaker", sorts, got)
		}
	}
}

func TestOrderByPutsMissingValuesLast(t *testing.T) {
	got := orderBy(testDescriptor, []shared.Sort{{Field: string(lead.SortLastActivityAt), Direction: shared.SortDesc}})
	if !strings.HasPrefix(got, testDescriptor.LastActivityExpr()+" DESC NULLS LAST") {
		t.Errorf("orderBy = %q, want NULLS LAST on the computed key", got)
	}
}

func TestOrderByDefaultsToNewestFirst(t *testing.T) {
	if got := orderBy(testDescriptor, nil); !strings.HasPrefix(got, "leads.created_at DESC") {
		t.Errorf("orderBy(testDescriptor, nil) = %q, want newest leads first", got)
	}
}

func TestOrderByIgnoresUnknownAndDuplicateKeys(t *testing.T) {
	got := orderBy(testDescriptor, []shared.Sort{
		{Field: "leads.id; DROP TABLE leads", Direction: shared.SortAsc},
	})
	if !strings.HasPrefix(got, "leads.created_at DESC") {
		t.Errorf("orderBy(unknown) = %q, want the default order", got)
	}
	if strings.Contains(got, "DROP") {
		t.Fatalf("unknown sort key reached the SQL: %q", got)
	}

	dup := orderBy(testDescriptor, []shared.Sort{
		{Field: string(lead.SortName), Direction: shared.SortAsc},
		{Field: string(lead.SortName), Direction: shared.SortDesc},
	})
	if strings.Count(dup, "NULLIF(leads.name, '')") != 1 {
		t.Errorf("orderBy(dup) = %q, want the repeated key collapsed", dup)
	}
}

func TestEverySortKeyResolvesToAnExpression(t *testing.T) {
	exprs := sortExpressions(testDescriptor)
	for _, key := range lead.AllSortKeys() {
		if exprs[key] == "" {
			t.Errorf("sort key %q has no SQL expression", key)
		}
	}
	if len(exprs) != len(lead.AllSortKeys()) {
		t.Errorf("sortExpressions has %d entries for %d declared keys", len(exprs), len(lead.AllSortKeys()))
	}
}

func TestCompiledQueryScopesWorkspaceAndSoftDeletes(t *testing.T) {
	q, err := newNilRepo().compile(lead.ListLeadsInput{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("compile() error = %v", err)
	}
	if !strings.Contains(q.where, "leads.workspace_id = ?") {
		t.Errorf("where = %q, want a workspace predicate", q.where)
	}
	if !strings.Contains(q.where, "leads.deleted_at IS NULL") {
		t.Errorf("where = %q, want a soft-delete guard", q.where)
	}
	if len(q.args) != 1 || q.args[0] != "ws-1" {
		t.Errorf("args = %v, want the workspace id", q.args)
	}
	if !strings.Contains(q.filteredIDs(), "SELECT leads.id FROM leads WHERE ") {
		t.Errorf("filteredIDs = %q, want the same WHERE the page uses", q.filteredIDs())
	}
}

var testDescriptor = infracrmfilter.LeadDescriptor{Alias: "leads", WorkspaceID: "ws-1"}

func TestOrderBySortsTheDenormalisedCountsByColumn(t *testing.T) {
	for key, column := range map[lead.SortKey]string{lead.SortRelatives: "leads.relatives_count", lead.SortReferred: "leads.referred_count"} {
		got := orderBy(testDescriptor, []shared.Sort{{Field: string(key), Direction: shared.SortDesc}})
		if !strings.HasPrefix(got, column+" DESC NULLS LAST") {
			t.Errorf("orderBy(%s) = %q, want the stored column", key, got)
		}
	}
}
