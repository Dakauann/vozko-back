package opportunity

import (
	"errors"
	"testing"
	"time"
)

var day = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func deal(id string, status Status, age int) *Opportunity {
	return &Opportunity{ID: id, Status: status, CreatedAt: day.AddDate(0, 0, -age)}
}

func TestEditableUsesTheOnlyOpenDeal(t *testing.T) {
	deals := EntryDeals{deal("won", StatusWon, 1), deal("open", StatusOpen, 5)}
	got, err := deals.Editable("")
	if err != nil || got.ID != "open" {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestEditableRefusesToGuessBetweenOpenDeals(t *testing.T) {
	deals := EntryDeals{deal("a", StatusOpen, 1), deal("b", StatusOpen, 2)}
	if _, err := deals.Editable(""); !errors.Is(err, ErrAmbiguousDeal) {
		t.Fatalf("two open deals must be ambiguous, got %v", err)
	}
	got, err := deals.Editable("b")
	if err != nil || got.ID != "b" {
		t.Fatalf("an explicit id resolves the ambiguity, got %v, %v", got, err)
	}
}

func TestEditableOnlyTouchesOpenDealsOfThisConversation(t *testing.T) {
	deals := EntryDeals{deal("won", StatusWon, 1), deal("open", StatusOpen, 2)}
	if _, err := deals.Editable("won"); !errors.Is(err, ErrDealClosed) {
		t.Fatalf("a closed deal cannot be changed by automation, got %v", err)
	}
	if _, err := deals.Editable("elsewhere"); !errors.Is(err, ErrDealNotLinked) {
		t.Fatalf("a deal of another conversation must be refused, got %v", err)
	}
	if _, err := (EntryDeals{deal("won", StatusWon, 1)}).Editable(""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no open deal means not found, got %v", err)
	}
}

func TestCurrentFallsBackToTheNewestClosedDeal(t *testing.T) {
	deals := EntryDeals{deal("old", StatusLost, 9), deal("new", StatusWon, 1)}
	got, err := deals.Current("")
	if err != nil || got.ID != "new" {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := (EntryDeals{}).Current(""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no deal means not found, got %v", err)
	}
}

func TestCurrentPrefersTheOpenDealAndReadsAnyLinkedDealById(t *testing.T) {
	deals := EntryDeals{deal("won", StatusWon, 1), deal("open", StatusOpen, 5)}
	if got, _ := deals.Current(""); got.ID != "open" {
		t.Fatalf("the open deal comes first, got %s", got.ID)
	}
	if got, err := deals.Current("won"); err != nil || got.ID != "won" {
		t.Fatalf("reading by id includes closed deals, got %v, %v", got, err)
	}
	if _, err := (EntryDeals{deal("a", StatusOpen, 1), deal("b", StatusOpen, 2)}).Current(""); !errors.Is(err, ErrAmbiguousDeal) {
		t.Fatalf("two open deals are ambiguous for reads too, got %v", err)
	}
}

func TestOpenCountsOnlyOpenDeals(t *testing.T) {
	deals := EntryDeals{deal("a", StatusOpen, 1), deal("b", StatusWon, 2), deal("c", StatusOpen, 3)}
	if len(deals.Open()) != 2 {
		t.Fatalf("open = %d", len(deals.Open()))
	}
}
