package asaas

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRefundSendsTheAmountInReaisWithItsCents(t *testing.T) {
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Fatalf("decode refund body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	gateway := NewGateway(newTestService(srv))
	if err := gateway.RefundCharge(context.Background(), "pay_abc123", 49.90, "devolucao"); err != nil {
		t.Fatalf("RefundCharge: %v", err)
	}
	if sent["value"] != 49.90 {
		t.Fatalf("refund value = %v, want 49.90 (cents must not be truncated)", sent["value"])
	}
}
