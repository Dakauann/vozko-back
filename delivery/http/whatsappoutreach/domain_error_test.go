package whatsappoutreach

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/balance"
)

func TestWriteDomainError_MonthlySendCapReached(t *testing.T) {
	rec := httptest.NewRecorder()

	(&Handler{}).writeDomainError(rec, fmt.Errorf("charge: %w", balance.ErrMonthlySendCapReached), nil)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "monthly_send_cap_reached") {
		t.Fatalf("body %s must carry the monthly_send_cap_reached code", rec.Body.String())
	}
}
