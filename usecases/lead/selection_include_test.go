package lead_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/crmfilter"
	"vozko/domain/selection"
)

const includedLead = "7d444840-9dc0-11d1-b245-5ffdce74fad2"

func TestIncludedLeadsReachTheReaderNormalizedWithTheirExclusions(t *testing.T) {
	reader := &fakeSelectionReader{ids: []string{includedLead}}
	r := selectionResolver(t, fakePermissions{"leads:read": true}, reader)
	s := selection.Selection{Mode: selection.ModeAllMatching, Filter: interestFilter(), IncludeIDs: []string{" 7D444840-9DC0-11D1-B245-5FFDCE74FAD2 ", includedLead}, ExcludeIDs: []string{pickedA}}

	if _, err := r.Resolve(context.Background(), scopeOf(operator()), s, "", 500); err != nil {
		t.Fatal(err)
	}
	q := reader.paged[0]
	if !reflect.DeepEqual(q.IncludeIDs, []string{includedLead}) || !reflect.DeepEqual(q.ExcludeIDs, []string{pickedA}) {
		t.Fatalf("the page read %+v", q)
	}

	s.IncludeIDs = []string{"not-a-lead"}
	if _, err := r.Resolve(context.Background(), scopeOf(operator()), s, "", 500); !errors.Is(err, selection.ErrInvalidIDs) {
		t.Fatalf("an invalid included id reached the reader: %v", err)
	}
}

func TestTheCountKeepsTheIncludedLeadsBeforeExclusions(t *testing.T) {
	reader := &fakeSelectionReader{count: 12}
	r := selectionResolver(t, fakePermissions{"leads:read": true}, reader)
	everyone := selection.Selection{Mode: selection.ModeEveryone, IncludeIDs: []string{includedLead}, ExcludeIDs: []string{pickedA}}

	if n, err := r.Count(context.Background(), scopeOf(operator()), everyone); err != nil || n != 12 {
		t.Fatalf("Count = %d, %v", n, err)
	}
	if q := reader.counted[0]; !reflect.DeepEqual(q.IncludeIDs, []string{includedLead}) || len(q.ExcludeIDs) != 0 || q.Limit != 0 {
		t.Fatalf("the count read %+v", q)
	}
}

func TestFirstNWithIncludedLeadsIsCountedWithinItsLimit(t *testing.T) {
	reader := &fakeSelectionReader{count: 26}
	r := selectionResolver(t, fakePermissions{"leads:read": true}, reader)
	s := selection.Selection{Mode: selection.ModeFirstN, Filter: interestFilter(), Limit: 25, Sort: []crmfilter.Sort{{Field: "name"}}, IncludeIDs: []string{includedLead}, ExcludeIDs: []string{pickedA}}

	selected, err := r.Selected(context.Background(), scopeOf(operator()), s)
	if err != nil || selected != 26 {
		t.Fatalf("Selected = %d, %v, want the 26 the reader counted past the limit", selected, err)
	}
	if q := reader.counted[0]; q.Limit != 25 || len(q.Order) == 0 || !reflect.DeepEqual(q.IncludeIDs, []string{includedLead}) || !reflect.DeepEqual(q.ExcludeIDs, []string{pickedA}) {
		t.Fatalf("Selected read %+v", q)
	}

	matched, err := r.Count(context.Background(), scopeOf(operator()), s.BeforeExclusions())
	if err != nil || s.ConfirmableCount(matched) != 26 {
		t.Fatalf("Count = %d, %v", matched, err)
	}
	if q := reader.counted[1]; q.Limit != 25 || len(q.Order) == 0 || len(q.ExcludeIDs) != 0 || !reflect.DeepEqual(q.IncludeIDs, []string{includedLead}) {
		t.Fatalf("the count read %+v", q)
	}
}
