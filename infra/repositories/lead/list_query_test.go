package lead

import (
	"strings"
	"testing"

	"vozko/domain/lead"
	"vozko/domain/shared"
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
		if got := orderBy(sorts); !strings.HasSuffix(got, ", leads.id DESC") {
			t.Errorf("orderBy(%v) = %q, want a leads.id tiebreaker", sorts, got)
		}
	}
}

func TestOrderByPutsMissingValuesLast(t *testing.T) {
	got := orderBy([]shared.Sort{{Field: string(lead.SortLastActivityAt), Direction: shared.SortDesc}})
	if !strings.Contains(got, "last_activity_at DESC NULLS LAST") {
		t.Errorf("orderBy = %q, want NULLS LAST on the computed key", got)
	}
}

func TestOrderByDefaultsToNewestFirst(t *testing.T) {
	if got := orderBy(nil); !strings.HasPrefix(got, "leads.created_at DESC") {
		t.Errorf("orderBy(nil) = %q, want newest leads first", got)
	}
}

func TestOrderByIgnoresUnknownAndDuplicateKeys(t *testing.T) {
	got := orderBy([]shared.Sort{
		{Field: "leads.id; DROP TABLE leads", Direction: shared.SortAsc},
	})
	if !strings.HasPrefix(got, "leads.created_at DESC") {
		t.Errorf("orderBy(unknown) = %q, want the default order", got)
	}
	if strings.Contains(got, "DROP") {
		t.Fatalf("unknown sort key reached the SQL: %q", got)
	}

	dup := orderBy([]shared.Sort{
		{Field: string(lead.SortName), Direction: shared.SortAsc},
		{Field: string(lead.SortName), Direction: shared.SortDesc},
	})
	if strings.Count(dup, "NULLIF(leads.name, '')") != 1 {
		t.Errorf("orderBy(dup) = %q, want the repeated key collapsed", dup)
	}
}

func TestEverySortKeyResolvesToAnExpression(t *testing.T) {
	exprs := sortExpressions()
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
