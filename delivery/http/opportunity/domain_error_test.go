package opportunity

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	opportunitydomain "vozko/domain/opportunity"
)

func TestADealOnAnotherLeadThanItsConversationIsABadRequest(t *testing.T) {
	rec := httptest.NewRecorder()
	(&OpportunityHandler{}).handleDomainError(rec, fmt.Errorf("create: %w", opportunitydomain.ErrEntryLeadMismatch))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
