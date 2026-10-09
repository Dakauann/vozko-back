package crmfilter

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/lib/pq"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
)

const (
	nameSet     = "SELECT l_q.id FROM leads l_q WHERE l_q.workspace_id = ? AND l_q.deleted_at IS NULL AND "
	nicknameSet = " UNION SELECT l_q.id FROM leads l_q WHERE l_q.workspace_id = ? AND l_q.nickname IS NOT NULL AND l_q.deleted_at IS NULL AND "
	placeSet    = " UNION SELECT la_q.lead_id FROM lead_addresses la_q WHERE la_q.workspace_id = ? AND la_q.is_primary AND "
	memorySet   = " UNION SELECT lm_q.lead_id FROM lead_memories lm_q WHERE lm_q.workspace_id = ? AND lm_q.deleted_at IS NULL AND "
	numberSet   = "SELECT l_q.id FROM leads l_q WHERE l_q.workspace_id = ? AND l_q.deleted_at IS NULL AND l_q.number "
	phoneSet    = " UNION SELECT lp_q.lead_id FROM lead_phones lp_q WHERE lp_q.workspace_id = ? AND lp_q.number "
)

func wordSet(word string) (string, []interface{}) {
	sql := nameSet + "vozko_fold(l_q.name) LIKE vozko_fold(?)" +
		nicknameSet + "vozko_fold(l_q.nickname) LIKE vozko_fold(?)" +
		placeSet + "(la_q.district_key LIKE ? OR la_q.city_key LIKE ?)" +
		memorySet + "vozko_fold(lm_q.content) LIKE vozko_fold(?)"
	pattern := "%" + word + "%"
	return sql, []interface{}{testWorkspace, pattern, testWorkspace, pattern, testWorkspace, pattern, pattern, testWorkspace, pattern}
}

func wordCheck(word string) (string, []interface{}) {
	sql := "(EXISTS (SELECT 1 FROM leads lc_q WHERE lc_q.id = s_q.id AND (vozko_fold(lc_q.name) LIKE vozko_fold(?) OR vozko_fold(lc_q.nickname) LIKE vozko_fold(?)))" +
		" OR EXISTS (SELECT 1 FROM lead_addresses lac_q WHERE lac_q.lead_id = s_q.id AND lac_q.is_primary AND (lac_q.district_key LIKE ? OR lac_q.city_key LIKE ?))" +
		" OR EXISTS (SELECT 1 FROM lead_memories lmc_q WHERE lmc_q.lead_id = s_q.id AND lmc_q.deleted_at IS NULL AND vozko_fold(lmc_q.content) LIKE vozko_fold(?)))"
	pattern := "%" + word + "%"
	return sql, []interface{}{pattern, pattern, pattern, pattern, pattern}
}

func TestOneWordFindsItsLeadsThroughTheIndexes(t *testing.T) {
	sql, args := compileLead(t, scopedLeadDesc(), pred(crmfilter.FieldQuery, crmfilter.OpContains, "Antônio"))
	set, setArgs := wordSet("antonio")
	if want := "(leads.id IN (" + set + "))"; sql != want {
		t.Fatalf("search SQL\n got: %s\nwant: %s", sql, want)
	}
	if !reflect.DeepEqual(args, setArgs) {
		t.Fatalf("args = %#v", args)
	}
}

func TestEveryWordOfALeadSearchMustMatchSomewhereAndTheLongestDrives(t *testing.T) {
	sql, args := compileLead(t, scopedLeadDesc(), pred(crmfilter.FieldQuery, crmfilter.OpContains, "Santo  Antônio"))
	antonio, antonioArgs := wordSet("antonio")
	santo, santoArgs := wordCheck("santo")
	want := "(leads.id IN (SELECT s_q.id FROM (" + antonio + ") s_q WHERE " + santo + "))"
	if sql != want {
		t.Fatalf("search SQL\n got: %s\nwant: %s", sql, want)
	}
	if !reflect.DeepEqual(args, append(antonioArgs, santoArgs...)) {
		t.Fatalf("args = %#v", args)
	}
	if strings.Count(sql, "?") != len(args) {
		t.Fatalf("%d placeholders for %d args", strings.Count(sql, "?"), len(args))
	}
}

func TestAShortWordMatchesTheStartOfAWord(t *testing.T) {
	sql, args := compileLead(t, scopedLeadDesc(), pred(crmfilter.FieldQuery, crmfilter.OpContains, "da"))
	for _, fragment := range []string{
		"(vozko_fold(l_q.name) LIKE vozko_fold(?) OR vozko_fold(l_q.name) LIKE vozko_fold(?))",
		"((la_q.district_key LIKE ? OR la_q.district_key LIKE ?) OR (la_q.city_key LIKE ? OR la_q.city_key LIKE ?))",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("missing %q in %s", fragment, sql)
		}
	}
	want := []interface{}{testWorkspace, "da%", "% da%", testWorkspace, "da%", "% da%", testWorkspace, "da%", "% da%", "%:da%", "% da%", testWorkspace, "da%", "% da%"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v", args)
	}
}

func TestABairroAbbreviationSearchesTheExpandedPlace(t *testing.T) {
	_, args := compileLead(t, scopedLeadDesc(), pred(crmfilter.FieldQuery, crmfilter.OpContains, "jd"))
	if !reflect.DeepEqual(args[6:9], []interface{}{testWorkspace, "%jardim%", "%jardim%"}) {
		t.Fatalf("place args = %#v", args)
	}
}

func TestAWholePhoneMatchesBothNinthDigitFormsExactly(t *testing.T) {
	sql, args := compileLead(t, scopedLeadDesc(), pred(crmfilter.FieldQuery, crmfilter.OpContains, "(84) 99999-1234"))
	want := "(leads.id IN (" + numberSet + "= ANY(?)" + phoneSet + "= ANY(?)))"
	if sql != want {
		t.Fatalf("phone SQL\n got: %s\nwant: %s", sql, want)
	}
	numbers := pq.Array([]string{"5584999991234", "558499991234"})
	if !reflect.DeepEqual(args, []interface{}{testWorkspace, numbers, testWorkspace, numbers}) {
		t.Fatalf("args = %#v", args)
	}
}

func TestANumberDrivesAMixedSearchAndTheWordsAreCheckedPerLead(t *testing.T) {
	sql, args := compileLead(t, scopedLeadDesc(), pred(crmfilter.FieldQuery, crmfilter.OpContains, "maria da 9999-1234"))
	phone := numberSet + "LIKE ?" + phoneSet + "LIKE ?"
	maria, _ := wordCheck("maria")
	shortName := " AND (EXISTS (SELECT 1 FROM leads lc_q WHERE lc_q.id = s_q.id AND ((vozko_fold(lc_q.name) LIKE vozko_fold(?) OR vozko_fold(lc_q.name) LIKE vozko_fold(?))"
	if !strings.HasPrefix(sql, "(leads.id IN (SELECT s_q.id FROM ("+phone+") s_q WHERE "+maria+shortName) {
		t.Fatalf("search SQL = %s", sql)
	}
	if strings.Count(sql, "?") != len(args) || args[1] != "%99991234%" {
		t.Fatalf("%d placeholders for args %#v", strings.Count(sql, "?"), args)
	}
}

func TestAPhoneWordIsCheckedOnBothNumbers(t *testing.T) {
	sql, args := compileLead(t, scopedLeadDesc(), pred(crmfilter.FieldQuery, crmfilter.OpContains, "maria 1234 5678"))
	want := " AND (EXISTS (SELECT 1 FROM leads lc_q WHERE lc_q.id = s_q.id AND lc_q.number LIKE ?) OR EXISTS (SELECT 1 FROM lead_phones lpc_q WHERE lpc_q.lead_id = s_q.id AND lpc_q.number LIKE ?))))"
	if !strings.HasSuffix(sql, want) || args[len(args)-1] != "%5678%" || args[1] != "%1234%" {
		t.Fatalf("search SQL = %s", sql)
	}
}

func TestAPartialNumberMatchesDigitsAnywhere(t *testing.T) {
	sql, args := compileLead(t, scopedLeadDesc(), pred(crmfilter.FieldQuery, crmfilter.OpContains, "9999-1234"))
	want := "(leads.id IN (" + numberSet + "LIKE ?" + phoneSet + "LIKE ?))"
	if sql != want {
		t.Fatalf("phone SQL\n got: %s\nwant: %s", sql, want)
	}
	if !reflect.DeepEqual(args, []interface{}{testWorkspace, "%99991234%", testWorkspace, "%99991234%"}) {
		t.Fatalf("args = %#v", args)
	}
}

func TestALeadSearchEscapesWildcards(t *testing.T) {
	_, args := compileLead(t, scopedLeadDesc(), pred(crmfilter.FieldQuery, crmfilter.OpContains, "d_1"))
	if args[1] != `%d\_1%` {
		t.Fatalf("pattern = %#v", args[1])
	}
}

func TestALeadSearchWithoutAUsableWordIsRefused(t *testing.T) {
	f := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{pred(crmfilter.FieldQuery, crmfilter.OpContains, "a")}}}}
	_, _, err := Compile(f, scopedLeadDesc(), 0)
	if !errors.Is(err, lead.ErrLeadSearchTooShort) {
		t.Fatalf("Compile = %v, want ErrLeadSearchTooShort", err)
	}
}

func TestALeadSearchNeedsTheWorkspace(t *testing.T) {
	f := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{pred(crmfilter.FieldQuery, crmfilter.OpContains, "maria")}}}}
	if _, _, err := Compile(f, leadDesc(), 0); !errors.Is(err, ErrWorkspaceScopeRequired) {
		t.Fatalf("Compile = %v, want ErrWorkspaceScopeRequired", err)
	}
}

func TestTheSearchOrderPutsNamePrefixesFirstThenSimilarity(t *testing.T) {
	f := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		pred(crmfilter.FieldBlocked, crmfilter.OpIsFalse),
		pred(crmfilter.FieldQuery, crmfilter.OpContains, "Maria 9999 Natal"),
	}}}}
	order, args, ok := scopedLeadDesc().SearchOrder(f)
	if !ok {
		t.Fatal("a text search must rank")
	}
	want := "(vozko_fold(leads.name) LIKE vozko_fold(?)) DESC NULLS LAST, public.similarity(vozko_fold(leads.name), vozko_fold(?)) DESC NULLS LAST"
	if order != want || !reflect.DeepEqual(args, []interface{}{"maria natal%", "maria natal"}) {
		t.Fatalf("order = %s %#v", order, args)
	}
	if _, _, ok := scopedLeadDesc().SearchOrder(crmfilter.Filter{Groups: []crmfilter.Group{{Predicates: []crmfilter.Predicate{pred(crmfilter.FieldQuery, crmfilter.OpContains, "84 99999 1234")}}}}); ok {
		t.Fatal("a phone search keeps the default order")
	}
	if _, _, ok := scopedLeadDesc().SearchOrder(crmfilter.Filter{}); ok {
		t.Fatal("no search keeps the default order")
	}
}
