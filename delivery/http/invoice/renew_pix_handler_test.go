package invoice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/gorilla/mux"

	invoicedomain "vozko/domain/invoice"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

type fakeRenewPix struct {
	input invoicedomain.RenewPixInput
	calls int
	out   *invoicedomain.Invoice
	err   error
}

func (f *fakeRenewPix) Execute(_ context.Context, input invoicedomain.RenewPixInput) (*invoicedomain.Invoice, error) {
	f.calls++
	f.input = input
	return f.out, f.err
}

func renewRouter(h *InvoiceHandler) *mux.Router {
	router := mux.NewRouter()
	RegisterUserRoutes(router, h, func(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Required-Permission", fmt.Sprintf("%s:%s", resource, action))
			next(w, r)
		}
	})
	return router
}

func renewRequest(workspaceID, invoiceID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/user/invoices/"+invoiceID+"/pix", nil)
	if workspaceID != "" {
		req = req.WithContext(context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, workspaceID))
	}
	return req
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return body
}

func renewedInvoice() *invoicedomain.Invoice {
	due := time.Now().Add(24 * time.Hour)
	expires := time.Now().Add(48 * time.Hour)
	qr, code := "qr-image", "000201pix"
	return &invoicedomain.Invoice{
		ID: "inv-1", WorkspaceID: "ws-1", Purpose: invoicedomain.PurposeMonthlyBilling, Status: invoicedomain.StatusOverdue,
		BillingType: "PIX", ExternalID: "pay_new", PixQrCode: &qr, PixCopy: &code, ChargeExpiresAt: &expires, DueDate: &due,
	}
}

func TestRenewingAPixNeedsTheInvoiceViewingPermission(t *testing.T) {
	uc := &fakeRenewPix{out: renewedInvoice()}
	rec := httptest.NewRecorder()
	renewRouter(&InvoiceHandler{renewPixUseCase: uc}).ServeHTTP(rec, renewRequest("ws-1", "inv-1"))

	want := fmt.Sprintf("%s:%s", workspace_domain.ResourceBalance, workspace_domain.ActionRead)
	if got := rec.Header().Get("X-Required-Permission"); got != want {
		t.Fatalf("POST /user/invoices/{id}/pix is guarded by %q, want %q", got, want)
	}
}

func TestARenewedPixIsReturnedWithItsPaymentDetails(t *testing.T) {
	uc := &fakeRenewPix{out: renewedInvoice()}
	rec := httptest.NewRecorder()
	renewRouter(&InvoiceHandler{renewPixUseCase: uc}).ServeHTTP(rec, renewRequest("ws-1", "inv-1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if uc.input != (invoicedomain.RenewPixInput{WorkspaceID: "ws-1", InvoiceID: "inv-1"}) {
		t.Fatalf("the renewal must be scoped to the caller's workspace, got %+v", uc.input)
	}
	inv := decodeBody(t, rec)["invoice"].(map[string]any)
	if inv["pixCopy"] != "000201pix" || inv["pixQrCode"] != "qr-image" {
		t.Fatalf("the payable code must be returned, got %v", inv)
	}
	pay := inv["payment"].(map[string]any)
	if pay["state"] != "OPEN" || pay["canRenew"] != false || pay["expiresAt"] == nil || pay["payableUntil"] == nil {
		t.Fatalf("payment = %v", pay)
	}
}

func TestARenewalWithoutAWorkspaceIsRefused(t *testing.T) {
	uc := &fakeRenewPix{}
	rec := httptest.NewRecorder()
	renewRouter(&InvoiceHandler{renewPixUseCase: uc}).ServeHTTP(rec, renewRequest("", "inv-1"))

	if rec.Code != http.StatusBadRequest || uc.calls != 0 {
		t.Fatalf("status = %d calls = %d, want 400 and no renewal", rec.Code, uc.calls)
	}
}

func TestRenewalRefusalsCarryStableCodes(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{invoicedomain.ErrInvoiceNotFound, http.StatusNotFound, "invoice_not_found"},
		{invoicedomain.ErrNotPayable, http.StatusConflict, "invoice_not_payable"},
		{invoicedomain.ErrAlreadyPaid, http.StatusConflict, "invoice_already_paid"},
		{invoicedomain.ErrPixRenewInProgress, http.StatusConflict, "invoice_pix_renew_in_progress"},
		{invoicedomain.ErrCustomerDocumentRequired, http.StatusUnprocessableEntity, "customer_document_required"},
		{fmt.Errorf("%w: provider down", invoicedomain.ErrPixUnavailable), http.StatusBadGateway, "invoice_pix_unavailable"},
		{errors.New("database down"), http.StatusInternalServerError, "invoice_pix_unavailable"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		renewRouter(&InvoiceHandler{renewPixUseCase: &fakeRenewPix{err: tc.err}}).ServeHTTP(rec, renewRequest("ws-1", "inv-1"))

		if rec.Code != tc.status {
			t.Errorf("%v: status = %d, want %d", tc.err, rec.Code, tc.status)
		}
		if got := decodeBody(t, rec)["code"]; got != tc.code {
			t.Errorf("%v: code = %v, want %s", tc.err, got, tc.code)
		}
	}
}

func TestTheRenewalCodesAreTheOnesTheFrontTranslates(t *testing.T) {
	want := []string{
		"invoice_not_found", "invoice_already_paid", "invoice_not_payable", "invoice_pix_renew_in_progress",
		"customer_document_required", "invoice_pix_unavailable",
	}
	if got := RenewPixRefusalCodes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("RenewPixRefusalCodes() = %v, want %v; update the invoicesPage.errors catalogs too", got, want)
	}
}

func TestSettledInvoicesNeverExposeAPayableCode(t *testing.T) {
	now := time.Now()
	for _, status := range []invoicedomain.Status{invoicedomain.StatusPaid, invoicedomain.StatusCancelled, invoicedomain.StatusExpired, invoicedomain.StatusRefunded, invoicedomain.StatusReversed} {
		inv := renewedInvoice()
		inv.Status = status
		slip := "https://provider.test/boleto"
		inv.BankSlipUrl = &slip

		resp := toInvoiceResponse(*inv, now)
		if resp.PixCopy != nil || resp.PixQrCode != nil || resp.BankSlipUrl != nil {
			t.Errorf("%s must expose no payable code, got %+v", status, resp)
		}
		if resp.Payment.State != string(invoicedomain.PaymentClosed) || resp.Payment.CanRenew {
			t.Errorf("%s payment = %+v, want CLOSED", status, resp.Payment)
		}
	}
}

func TestAnExpiredPixIsOfferedForRenewalWithoutItsCode(t *testing.T) {
	inv := renewedInvoice()
	expired := time.Now().Add(-time.Hour)
	inv.ChargeExpiresAt = &expired

	resp := toInvoiceResponse(*inv, time.Now())
	if resp.PixCopy != nil || resp.PixQrCode != nil {
		t.Fatal("an expired code must not be shown")
	}
	if resp.Payment.State != string(invoicedomain.PaymentRenewable) || !resp.Payment.CanRenew {
		t.Fatalf("payment = %+v, want RENEWABLE", resp.Payment)
	}
}
