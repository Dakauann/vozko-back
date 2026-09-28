package opportunity

import (
	"errors"
	"testing"
)

func TestADealOnlyAcceptsChangesBasedOnItsCurrentVersion(t *testing.T) {
	deal := &Opportunity{Version: 3}
	current, stale := int64(3), int64(2)
	if err := deal.Expects(&current); err != nil {
		t.Fatalf("current version refused: %v", err)
	}
	if err := deal.Expects(&stale); !errors.Is(err, ErrStaleDeal) {
		t.Fatalf("stale version error = %v", err)
	}
	if err := deal.Expects(nil); err != nil {
		t.Fatalf("no expectation must not block server-side reads: %v", err)
	}
}
