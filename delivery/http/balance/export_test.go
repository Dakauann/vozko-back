package balance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/auth"
	balancedomain "vozko/domain/balance"
	"vozko/infra/http/middleware"
)

type exportGetOrCreateStub struct{}

func (exportGetOrCreateStub) Execute(workspaceID string) (*balancedomain.Balance, error) {
	return &balancedomain.Balance{ID: "bal-1", WorkspaceID: workspaceID, Currency: "USD"}, nil
}

func exportRequest(query string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/user/balance/transactions/export"+query, nil)
	ctx := context.WithValue(req.Context(), middleware.ClaimsContextKey,
		&auth.Claims{UserID: "user-1", Role: "member"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
	return req.WithContext(ctx)
}

func TestExportMyTransactionsRejectsAnInvalidServiceType(t *testing.T) {
	handler := &BalanceHandler{getOrCreateUseCase: exportGetOrCreateStub{}}

	recorder := httptest.NewRecorder()
	handler.ExportMyTransactions(recorder, exportRequest("?serviceType=not-a-service"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if recorder.Body.Len() == 0 {
		t.Fatal("a rejected filter must say which field was wrong")
	}
}

func TestExportMyTransactionsRejectsAnUnparseableDate(t *testing.T) {
	handler := &BalanceHandler{getOrCreateUseCase: exportGetOrCreateStub{}}

	recorder := httptest.NewRecorder()
	handler.ExportMyTransactions(recorder, exportRequest("?startDate=last-tuesday"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: a bad date must never silently widen the export",
			recorder.Code, http.StatusBadRequest)
	}
}

func TestExportMyTransactionsRefusesWhenReportsAreNotConfigured(t *testing.T) {
	handler := &BalanceHandler{getOrCreateUseCase: exportGetOrCreateStub{}}

	recorder := httptest.NewRecorder()
	handler.ExportMyTransactions(recorder, exportRequest(""))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

func TestExportMyTransactionsRequiresAuthentication(t *testing.T) {
	handler := &BalanceHandler{getOrCreateUseCase: exportGetOrCreateStub{}}

	req := httptest.NewRequest(http.MethodGet, "/user/balance/transactions/export", nil)
	recorder := httptest.NewRecorder()
	handler.ExportMyTransactions(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}
