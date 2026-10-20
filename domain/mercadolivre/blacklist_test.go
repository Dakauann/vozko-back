package mercadolivre

import (
	"reflect"
	"strconv"
	"testing"
	"time"
)

func scanPages(pages ...BlacklistPage) *BlacklistScan {
	scan := NewBlacklistScan()
	for _, p := range pages {
		if _, more := scan.Add(p); !more {
			break
		}
	}
	return scan
}

func TestABlacklistScanIsCompleteOnlyWhenEveryBlockedUserWasRead(t *testing.T) {
	cases := []struct {
		name     string
		pages    []BlacklistPage
		complete bool
		ids      []string
	}{
		{"empty blacklist", []BlacklistPage{{Total: 0}}, true, nil},
		{"one full page", []BlacklistPage{{UserIDs: []string{"70927648", "170577643"}, Total: 2}}, true, []string{"70927648", "170577643"}},
		{"two pages", []BlacklistPage{
			{UserIDs: []string{"1", "2"}, Total: 3},
			{UserIDs: []string{"3"}, Total: 3, Offset: 2},
		}, true, []string{"1", "2", "3"}},
		{"total moved between pages", []BlacklistPage{
			{UserIDs: []string{"1", "2"}, Total: 3},
			{UserIDs: []string{"3"}, Total: 4, Offset: 2},
		}, false, []string{"1", "2", "3"}},
		{"a page ended early", []BlacklistPage{
			{UserIDs: []string{"1", "2"}, Total: 3},
			{Total: 3, Offset: 2},
		}, false, []string{"1", "2"}},
		{"an id that is not numeric", []BlacklistPage{{UserIDs: []string{"1", "x"}, Total: 2}}, false, []string{"1"}},
		{"a repeated id", []BlacklistPage{{UserIDs: []string{"1", "1"}, Total: 2}}, false, []string{"1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scan := scanPages(tc.pages...)
			if scan.Complete() != tc.complete {
				t.Fatalf("complete = %v", scan.Complete())
			}
			if got := scan.UserIDs(); !reflect.DeepEqual(got, tc.ids) && !(len(got) == 0 && len(tc.ids) == 0) {
				t.Fatalf("ids = %v", got)
			}
		})
	}
}

func TestABlacklistScanAsksForTheNextOffsetAndStopsAtItsBound(t *testing.T) {
	scan := NewBlacklistScan()
	next, more := scan.Add(BlacklistPage{UserIDs: []string{"1", "2"}, Total: 5})
	if !more || next != 2 {
		t.Fatalf("next = %d more = %v", next, more)
	}
	bounded := NewBlacklistScan()
	pages := 0
	for {
		pages++
		ids := make([]string, MaxBlacklistLimit)
		for i := range ids {
			ids[i] = strconv.Itoa(pages*1000 + i + 1)
		}
		if _, more := bounded.Add(BlacklistPage{UserIDs: ids, Total: MaxBlacklistLimit * (MaxBlacklistPages + 5)}); !more {
			break
		}
	}
	if pages != MaxBlacklistPages || bounded.Complete() {
		t.Fatalf("pages = %d complete = %v", pages, bounded.Complete())
	}
}

func TestAContactKnowsWhetherItsQuestionsAreBlocked(t *testing.T) {
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	if (&Contact{}).QuestionsBlocked() {
		t.Fatal("a contact without a block time is not blocked")
	}
	if !(&Contact{QuestionsBlockedAt: &at}).QuestionsBlocked() {
		t.Fatal("a contact with a block time is blocked")
	}
}
