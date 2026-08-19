package lead

import (
	"encoding/base64"
	"net/url"
	"testing"

	"vozko/domain/crmfilter"
	leaddomain "vozko/domain/lead"
	"vozko/domain/shared"
)

func parse(t *testing.T, raw string) leaddomain.ListLeadsInput {
	t.Helper()
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", raw, err)
	}
	input, err := listInputFromQuery("ws-1", values)
	if err != nil {
		t.Fatalf("listInputFromQuery(%q) error = %v", raw, err)
	}
	return input
}

// find returns the single predicate on a field, or fails.
func find(t *testing.T, input leaddomain.ListLeadsInput, field crmfilter.Field) crmfilter.Predicate {
	t.Helper()
	var found []crmfilter.Predicate
	for _, g := range input.Filter.Groups {
		for _, p := range g.Predicates {
			if p.Field == field {
				found = append(found, p)
			}
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one %q predicate, got %d", field, len(found))
	}
	return found[0]
}

// Everything the query string can say must survive as a predicate, because the
// repository no longer has a second scalar path to fall back on: a parameter
// that does not become a predicate here becomes a filter that silently does
// nothing.
func TestFlatParamsBecomePredicates(t *testing.T) {
	input := parse(t, "q=boleto&number=5511&name=ana&ageFrom=25&ageTo=40"+
		"&blocked=false&windowOpen=true&channel=telegram,instagram"+
		"&hasWhatsAppCampaign=true&campaignStatus=FAILED&stageId=st-1&labelId=lb-1"+
		"&hasMemory=true&memoryCategory=deal&memoryAuthor=ai&memoryText=prazo"+
		"&memoriesFrom=2&createdFrom=2026-01-01&activityTo=2026-02-01")

	cases := []struct {
		field  crmfilter.Field
		op     crmfilter.Operator
		values []string
	}{
		{crmfilter.FieldQuery, crmfilter.OpContains, []string{"boleto"}},
		{crmfilter.FieldNumber, crmfilter.OpContains, []string{"5511"}},
		{crmfilter.FieldName, crmfilter.OpContains, []string{"ana"}},
		{crmfilter.FieldBlocked, crmfilter.OpIsFalse, nil},
		{crmfilter.FieldWindowOpen, crmfilter.OpIsTrue, nil},
		{crmfilter.FieldChannel, crmfilter.OpIn, []string{"telegram", "instagram"}},
		{crmfilter.FieldCampaignStatus, crmfilter.OpIn, []string{"FAILED"}},
		{crmfilter.FieldStage, crmfilter.OpIn, []string{"st-1"}},
		{crmfilter.FieldLabel, crmfilter.OpIn, []string{"lb-1"}},
		{crmfilter.FieldMemoryAuthor, crmfilter.OpIn, []string{"ai"}},
		{crmfilter.FieldMemoryText, crmfilter.OpContains, []string{"prazo"}},
		{crmfilter.FieldMemoryCount, crmfilter.OpGreaterEq, []string{"2"}},
	}
	for _, tc := range cases {
		p := find(t, input, tc.field)
		if p.Operator != tc.op {
			t.Errorf("%s operator = %q, want %q", tc.field, p.Operator, tc.op)
		}
		if len(tc.values) > 0 && len(p.Values) != len(tc.values) {
			t.Errorf("%s values = %v, want %v", tc.field, p.Values, tc.values)
			continue
		}
		for i := range tc.values {
			if p.Values[i] != tc.values[i] {
				t.Errorf("%s value %d = %q, want %q", tc.field, i, p.Values[i], tc.values[i])
			}
		}
	}

	// The whole expression must validate: the repository compiles it verbatim,
	// and an invalid predicate there is a 400 for a query the UI can build.
	if err := input.Filter.Validate(); err != nil {
		t.Fatalf("assembled filter does not validate: %v", err)
	}
}

// An absent boolean is "no opinion", not false. Defaulting it would make the
// unfiltered list silently hide every blocked lead.
func TestAbsentBooleansAddNoPredicate(t *testing.T) {
	input := parse(t, "page=1")
	if !input.Filter.IsEmpty() {
		t.Errorf("empty query produced predicates: %+v", input.Filter)
	}

	for _, raw := range []string{"blocked=", "blocked=maybe", "windowOpen=xyz"} {
		if got := parse(t, raw); !got.Filter.IsEmpty() {
			t.Errorf("%q produced predicates: %+v", raw, got.Filter)
		}
	}
}

// hasX=true|false is a presence question on a membership field, not equality.
func TestPresenceParamsMapToSetEmptiness(t *testing.T) {
	if p := find(t, parse(t, "hasWhatsAppCampaign=true"), crmfilter.FieldCampaign); p.Operator != crmfilter.OpIsSet {
		t.Errorf("hasWhatsAppCampaign=true operator = %q, want is_set", p.Operator)
	}
	if p := find(t, parse(t, "hasWhatsAppCampaign=false"), crmfilter.FieldCampaign); p.Operator != crmfilter.OpIsEmpty {
		t.Errorf("hasWhatsAppCampaign=false operator = %q, want is_empty", p.Operator)
	}
	if p := find(t, parse(t, "hasMemory=false"), crmfilter.FieldMemoryCategory); p.Operator != crmfilter.OpIsEmpty {
		t.Errorf("hasMemory=false operator = %q, want is_empty", p.Operator)
	}
	if p := find(t, parse(t, "hasName=false"), crmfilter.FieldName); p.Operator != crmfilter.OpIsEmpty {
		t.Errorf("hasName=false operator = %q, want is_empty", p.Operator)
	}
}

// A date-only upper bound has to cover the whole day, or "até 31/07" drops
// everything that happened on the 31st.
func TestDateOnlyUpperBoundCoversTheWholeDay(t *testing.T) {
	from := find(t, parse(t, "createdFrom=2026-07-01"), crmfilter.FieldCreatedAt)
	if want := "2026-07-01T00:00:00Z"; from.Values[0] != want {
		t.Errorf("createdFrom = %q, want %q", from.Values[0], want)
	}

	to := find(t, parse(t, "createdTo=2026-07-31"), crmfilter.FieldCreatedAt)
	parsed, err := crmfilter.ParseDate(to.Values[0])
	if err != nil {
		t.Fatalf("createdTo did not round-trip: %v", err)
	}
	if parsed.Day() != 31 || parsed.Hour() != 23 {
		t.Errorf("createdTo = %q, want the final instant of the 31st", to.Values[0])
	}
}

// Both filter shapes must survive together: a structured expression from the
// new UI plus whatever flat parameters the URL still carries.
func TestStructuredFilterMergesWithFlatParams(t *testing.T) {
	structured := `{"groups":[{"conjunction":"or","predicates":[` +
		`{"field":"memory_category","operator":"in","values":["deal"]},` +
		`{"field":"memory_category","operator":"in","values":["objection"]}]}]}`
	encoded := base64.StdEncoding.EncodeToString([]byte(structured))

	input := parse(t, "blocked=false&filter="+url.QueryEscape(encoded))

	if len(input.Filter.Groups) != 2 {
		t.Fatalf("groups = %d, want the flat predicate group plus the structured one", len(input.Filter.Groups))
	}
	if err := input.Filter.Validate(); err != nil {
		t.Fatalf("merged filter does not validate: %v", err)
	}

	var orGroup *crmfilter.Group
	for i := range input.Filter.Groups {
		if input.Filter.Groups[i].Conj() == crmfilter.Or {
			orGroup = &input.Filter.Groups[i]
		}
	}
	if orGroup == nil || len(orGroup.Predicates) != 2 {
		t.Fatal("the structured OR group did not survive the merge")
	}
}

// Plain JSON must work too: base64 is a convenience for URL-safety, not a
// contract a hand-written request has to satisfy.
func TestStructuredFilterAcceptsPlainJSON(t *testing.T) {
	raw := `{"groups":[{"predicates":[{"field":"blocked","operator":"is_true"}]}]}`
	input := parse(t, "filter="+url.QueryEscape(raw))
	if p := find(t, input, crmfilter.FieldBlocked); p.Operator != crmfilter.OpIsTrue {
		t.Errorf("operator = %q, want is_true", p.Operator)
	}
}

func TestMalformedFilterIsRejected(t *testing.T) {
	values := url.Values{"filter": []string{"{not json"}}
	if _, err := listInputFromQuery("ws-1", values); err == nil {
		t.Error("listInputFromQuery accepted malformed filter JSON")
	}
}

// The sort vocabulary is derived from the domain, so every declared key is
// reachable over HTTP. The old hand-written map is why lastActivity was
// offered by the UI and ignored by the server.
func TestEverySortKeyIsAcceptedOverHTTP(t *testing.T) {
	for _, key := range leaddomain.AllSortKeys() {
		input := parse(t, "sort="+string(key)+":desc")
		if len(input.Options.Sorts) != 1 {
			t.Fatalf("sort=%s produced %d sorts", key, len(input.Options.Sorts))
		}
		if input.Options.Sorts[0].Field != string(key) {
			t.Errorf("sort=%s resolved to %q", key, input.Options.Sorts[0].Field)
		}
		if input.Options.Sorts[0].Direction != shared.SortDesc {
			t.Errorf("sort=%s:desc direction = %q", key, input.Options.Sorts[0].Direction)
		}
	}
}

func TestSortSupportsMultipleKeysAndLegacyOrderParam(t *testing.T) {
	input := parse(t, "sort=lastActivityAt:desc,name")
	if len(input.Options.Sorts) != 2 {
		t.Fatalf("sorts = %d, want 2", len(input.Options.Sorts))
	}
	if input.Options.Sorts[1].Direction != shared.SortAsc {
		t.Errorf("second key defaulted to %q, want asc", input.Options.Sorts[1].Direction)
	}

	// Legacy shape: direction in its own parameter.
	legacy := parse(t, "sort=name&order=desc")
	if len(legacy.Options.Sorts) != 1 || legacy.Options.Sorts[0].Direction != shared.SortDesc {
		t.Errorf("legacy order param ignored: %+v", legacy.Options.Sorts)
	}

	// An explicit ":dir" wins over the blanket order param.
	mixed := parse(t, "sort=name:asc&order=desc")
	if mixed.Options.Sorts[0].Direction != shared.SortAsc {
		t.Errorf("explicit direction was overridden by order=desc")
	}
}

func TestUnknownSortKeyIsDroppedNotRejected(t *testing.T) {
	input := parse(t, "sort=favouriteColour:desc")
	if len(input.Options.Sorts) != 0 {
		t.Errorf("unknown sort key produced %+v, want none (the repository then defaults)", input.Options.Sorts)
	}
}

// The page carries five correlated subqueries per row; an uncapped pageSize is
// a way to ask one request to do unbounded work.
func TestPageSizeIsCapped(t *testing.T) {
	input := parse(t, "pageSize=100000")
	if input.Options.Pagination.PageSize != leadListMaxPageSize {
		t.Errorf("pageSize = %d, want the %d cap", input.Options.Pagination.PageSize, leadListMaxPageSize)
	}

	if got := parse(t, "pageSize=50").Options.Pagination.PageSize; got != 50 {
		t.Errorf("pageSize = %d, want 50 honoured below the cap", got)
	}
}

func TestWorkspaceTravelsWithTheQuery(t *testing.T) {
	if got := parse(t, "").WorkspaceID; got != "ws-1" {
		t.Errorf("WorkspaceID = %q, want ws-1", got)
	}
}
