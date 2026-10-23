package asaas

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	billing "vozko/domain/billing"
	"vozko/domain/payment"
)

func asaasServer(t *testing.T, routes map[string]func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			t.Errorf("unexpected Asaas call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func TestAPixChargeCarriesTheExpiryOfItsQrCode(t *testing.T) {
	var sent map[string]any
	srv := asaasServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /customers": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{"data": []map[string]string{{"id": "cus_1"}}})
		},
		"POST /payments": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&sent)
			writeJSON(w, map[string]any{"id": "pay_1", "status": "PENDING", "dueDate": "2026-10-27", "value": 210})
		},
		"GET /payments/pay_1/pixQrCode": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]string{"encodedImage": "qr-image", "payload": "000201pix", "expirationDate": "2026-10-27 23:59:59"})
		},
	})

	charge, err := NewGateway(newTestService(srv)).CreateCharge(context.Background(), payment.ChargeRequest{
		Method:            payment.MethodPix,
		Amount:            210,
		DueDate:           time.Date(2026, time.October, 27, 23, 59, 59, 0, billing.LocationBRT()),
		ExternalReference: "inv:abc",
		Customer:          payment.GatewayCustomer{Name: "Maria", Document: "11144477735"},
	})
	if err != nil {
		t.Fatalf("CreateCharge: %v", err)
	}

	want := time.Date(2026, time.October, 27, 23, 59, 59, 0, billing.LocationBRT())
	if charge.ExpiresAt == nil || !charge.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", charge.ExpiresAt, want)
	}
	if charge.PixCopyPaste != "000201pix" || charge.PixQRCodeBase64 != "qr-image" {
		t.Fatalf("PIX payload not mapped: %+v", charge)
	}
	if _, sentStatus := sent["status"]; sentStatus {
		t.Fatal("a create request must never send a status")
	}
	if _, sentDeleted := sent["deleted"]; sentDeleted {
		t.Fatal("a create request must never send the deleted flag")
	}
}

func TestAQrCodeWithoutAnExpiryLeavesItUnknown(t *testing.T) {
	srv := asaasServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /customers": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{"data": []map[string]string{{"id": "cus_1"}}})
		},
		"POST /payments": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{"id": "pay_1", "status": "PENDING", "dueDate": "2026-10-27"})
		},
		"GET /payments/pay_1/pixQrCode": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]string{"encodedImage": "qr-image", "payload": "000201pix"})
		},
	})

	charge, err := NewGateway(newTestService(srv)).CreateCharge(context.Background(), payment.ChargeRequest{
		Method: payment.MethodPix, Amount: 10, Customer: payment.GatewayCustomer{Name: "Maria", Document: "11144477735"},
	})
	if err != nil {
		t.Fatalf("CreateCharge: %v", err)
	}
	if charge.ExpiresAt != nil {
		t.Fatalf("an unknown expiry must stay unknown, got %v", charge.ExpiresAt)
	}
}

func TestTheChargeStatusComesFromAsaas(t *testing.T) {
	cases := []struct {
		body map[string]any
		want payment.Status
	}{
		{map[string]any{"id": "pay_1", "status": "PENDING"}, payment.StatusPending},
		{map[string]any{"id": "pay_1", "status": "OVERDUE"}, payment.StatusOverdue},
		{map[string]any{"id": "pay_1", "status": "RECEIVED"}, payment.StatusReceived},
		{map[string]any{"id": "pay_1", "status": "CONFIRMED"}, payment.StatusReceived},
		{map[string]any{"id": "pay_1", "status": "RECEIVED_IN_CASH"}, payment.StatusReceivedInCash},
		{map[string]any{"id": "pay_1", "status": "REFUNDED"}, payment.StatusRefunded},
		{map[string]any{"id": "pay_1", "status": "PENDING", "deleted": true}, payment.StatusCancelled},
		{map[string]any{"id": "pay_1", "status": "CHARGEBACK_REQUESTED"}, payment.Status("CHARGEBACK_REQUESTED")},
	}
	for _, tc := range cases {
		srv := asaasServer(t, map[string]func(http.ResponseWriter, *http.Request){
			"GET /payments/pay_1": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, tc.body) },
		})
		charge, err := NewGateway(newTestService(srv)).GetCharge(context.Background(), "pay_1")
		if err != nil {
			t.Fatalf("GetCharge(%v): %v", tc.body, err)
		}
		if charge.Status != tc.want {
			t.Errorf("GetCharge(%v).Status = %q, want %q", tc.body, charge.Status, tc.want)
		}
	}
}

func TestAChargeAsaasDoesNotKnowIsNotFound(t *testing.T) {
	srv := asaasServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /payments/pay_gone": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
	})
	_, err := NewGateway(newTestService(srv)).GetCharge(context.Background(), "pay_gone")
	if !errors.Is(err, payment.ErrChargeNotFound) {
		t.Fatalf("GetCharge of an unknown charge = %v, want ErrChargeNotFound", err)
	}
}

func TestAnyOtherFailureReadingAChargeIsNotANotFound(t *testing.T) {
	srv := asaasServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /payments/pay_1": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
	})
	_, err := NewGateway(newTestService(srv)).GetCharge(context.Background(), "pay_1")
	if err == nil || errors.Is(err, payment.ErrChargeNotFound) || !strings.Contains(err.Error(), "401") {
		t.Fatalf("GetCharge with bad credentials = %v, want a plain failure", err)
	}
}
