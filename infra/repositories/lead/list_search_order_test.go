package lead

import (
	"strings"
	"testing"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

func searchInput(q string) lead.ListLeadsInput {
	return lead.ListLeadsInput{
		WorkspaceID: pageWorkspace,
		Filter: crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
			{Field: crmfilter.FieldQuery, Operator: crmfilter.OpContains, Values: []string{q}},
		}}}},
	}
}

func TestASearchWithoutAChosenSortRanksNamePrefixesThenSimilarity(t *testing.T) {
	q, err := newNilRepo().compile(searchInput("Maria Silva"))
	if err != nil {
		t.Fatal(err)
	}
	sql, args := q.pageIDs(shared.QueryOptions{Pagination: shared.Pagination{Page: 2, PageSize: 20}})
	want := " ORDER BY (vozko_fold(leads.name) LIKE vozko_fold(?)) DESC NULLS LAST, public.similarity(vozko_fold(leads.name), vozko_fold(?)) DESC NULLS LAST, leads.created_at DESC NULLS LAST, leads.id DESC LIMIT ? OFFSET ?"
	if !strings.HasSuffix(sql, want) {
		t.Fatalf("id read = %s", sql)
	}
	if strings.Count(sql, "?") != len(args) {
		t.Fatalf("%d placeholders for %d args", strings.Count(sql, "?"), len(args))
	}
	tail := args[len(args)-4:]
	if tail[0] != "maria silva%" || tail[1] != "maria silva" || tail[2] != 20 || tail[3] != 20 {
		t.Fatalf("order and page args = %#v", tail)
	}
}

func TestAChosenSortWinsOverTheSearchRank(t *testing.T) {
	q, err := newNilRepo().compile(searchInput("Maria"))
	if err != nil {
		t.Fatal(err)
	}
	sql, args := q.pageIDs(shared.QueryOptions{Sorts: []shared.Sort{{Field: string(lead.SortName), Direction: shared.SortAsc}}})
	if strings.Contains(sql, "similarity") || strings.Count(sql, "?") != len(args) {
		t.Fatalf("an explicit sort must not rank: %s", sql)
	}
}

func TestAPhoneSearchKeepsTheDefaultOrder(t *testing.T) {
	q, err := newNilRepo().compile(searchInput("84 99999-1234"))
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := q.pageIDs(shared.QueryOptions{})
	if !strings.Contains(sql, " ORDER BY leads.created_at DESC NULLS LAST, leads.id DESC LIMIT") {
		t.Fatalf("id read = %s", sql)
	}
}
