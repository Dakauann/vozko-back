package advertising

import (
	"context"
	"slices"
	"testing"

	ads "vozko/domain/advertising"
)

func TestRefreshReviewsRereadsOnlyAccountsWithAnAdInReview(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	w.objects.byID["a-1"].EffectiveStatus = ads.EffectivePendingReview
	if err := w.sync.RefreshReviews(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(w.gateway.calls, "list_ad") {
		t.Fatalf("calls %v, want the structure re-read", w.gateway.calls)
	}
}

func TestRefreshReviewsStaysQuietWhenNothingIsInReview(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	if err := w.sync.RefreshReviews(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.gateway.calls) != 0 {
		t.Fatalf("calls %v, want none", w.gateway.calls)
	}
}
