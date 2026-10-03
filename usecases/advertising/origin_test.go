package advertising

import (
	"context"
	"errors"
	"testing"
	"time"

	ads "vozko/domain/advertising"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type fakeEntryAccess struct{ allowed bool }

func (f fakeEntryAccess) CanAccessEntry(string, string, string, string, bool) bool { return f.allowed }

type fakeOrigins struct{ origin *conversation.AdOrigin }

func (f fakeOrigins) AdOrigin(string, shared.EntryType) (*conversation.AdOrigin, error) {
	return f.origin, nil
}

func originUseCase(w *world, allowed bool, origin *conversation.AdOrigin) *OriginUseCase {
	return NewOriginUseCase(fakeEntryAccess{allowed: allowed}, fakeOrigins{origin: origin}, w.accounts, w.objects, w.insights, w.attrib)
}

var person = shared.Person{UserID: "u-1"}

func TestOriginEstimatesTheLeadCostFromThatDaysSpend(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	w.attrib.conversations = 2
	arrived := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
	got, err := originUseCase(w, true, &conversation.AdOrigin{AdID: "a-1", ArrivedAt: arrived}).
		Origin(context.Background(), person, "ws-1", "whatsapp", "entry-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Campaign.MetaID != "c-1" || got.AdSet.MetaID != "s-1" || *got.Cost.Estimate() != 15_000_000 {
		t.Fatalf("origin %+v cost %+v", got, got.Cost)
	}
}

func TestOriginWithoutSpendThatDayHasNoEstimate(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	got, err := originUseCase(w, true, &conversation.AdOrigin{AdID: "a-1", ArrivedAt: time.Date(2026, 9, 10, 14, 0, 0, 0, time.UTC)}).
		Origin(context.Background(), person, "ws-1", "whatsapp", "entry-1")
	if err != nil || got.Cost.Estimate() != nil {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestOriginOfAConversationThePersonCannotSeeIsRefused(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	_, err := originUseCase(w, false, &conversation.AdOrigin{AdID: "a-1"}).Origin(context.Background(), person, "ws-1", "whatsapp", "entry-1")
	if !errors.Is(err, ErrConversationNotVisible) {
		t.Fatalf("err %v", err)
	}
}

func TestOriginFromAnAdOutsideTheWorkspacesAccountsIsNotFound(t *testing.T) {
	w := newWorld()
	_, err := originUseCase(w, true, &conversation.AdOrigin{AdID: "unknown"}).Origin(context.Background(), person, "ws-1", "whatsapp", "entry-1")
	if !errors.Is(err, ads.ErrObjectNotFound) {
		t.Fatalf("err %v", err)
	}
}
