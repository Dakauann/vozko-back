package balance

import (
	"context"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"vozko/domain/auth"
	balancedomain "vozko/domain/balance"
	"vozko/domain/shared"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	"vozko/infra/http/middleware"
)

type exportGetOrCreateStub struct{}

func (exportGetOrCreateStub) Execute(workspaceID string) (*balancedomain.Balance, error) {
	return &balancedomain.Balance{ID: "bal-1", WorkspaceID: workspaceID, Currency: "USD"}, nil
}

type exportListTransactionsStub struct {
	t       *testing.T
	pages   []shared.Pagination
	results map[int]*shared.PaginatedResult[*balancedomain.Transaction]
}

func (s *exportListTransactionsStub) Execute(input balancedomain.ListTransactionsInput) (*shared.PaginatedResult[*balancedomain.Transaction], error) {
	if input.WorkspaceID != "ws-1" {
		s.t.Fatalf("workspace mismatch: got %q", input.WorkspaceID)
	}
	s.pages = append(s.pages, input.Pagination)
	if result, ok := s.results[input.Pagination.Page]; ok {
		return result, nil
	}
	return &shared.PaginatedResult[*balancedomain.Transaction]{
		Items:      nil,
		Page:       input.Pagination.Page,
		PageSize:   input.Pagination.PageSize,
		TotalItems: 150,
		TotalPages: 1,
	}, nil
}

type exportExchangeRateStub struct{}

func (exportExchangeRateStub) Execute() (*workspace_pricing.PricingItem, error) {
	return &workspace_pricing.PricingItem{PriceMicros: 6_000_000, Currency: "BRL"}, nil
}

func TestExportMyTransactions_PaginatesPastReportedFirstPageAndFormatsBRL(t *testing.T) {
	pageOneItems := buildExportTransactions(0, 100)
	pageTwoItems := buildExportTransactions(100, 50)

	listUC := &exportListTransactionsStub{
		t: t,
		results: map[int]*shared.PaginatedResult[*balancedomain.Transaction]{
			1: {
				Items:      pageOneItems,
				Page:       1,
				PageSize:   shared.MaxPageSize,
				TotalItems: 150,
				TotalPages: 1,
			},
			2: {
				Items:      pageTwoItems,
				Page:       2,
				PageSize:   shared.MaxPageSize,
				TotalItems: 150,
				TotalPages: 1,
			},
		},
	}

	handler := &BalanceHandler{
		getOrCreateUseCase:      exportGetOrCreateStub{},
		listTransactionsUseCase: listUC,
		getExchangeRateUseCase:  exportExchangeRateStub{},
	}

	req := httptest.NewRequest(http.MethodGet, "/user/balance/transactions/export?format=csv", nil)
	ctx := context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "member"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()
	handler.ExportMyTransactions(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/csv" {
		t.Fatalf("content type = %q, want %q", got, "text/csv")
	}
	if len(listUC.pages) != 2 {
		t.Fatalf("expected 2 export pages, got %d", len(listUC.pages))
	}
	if listUC.pages[0].Page != 1 || listUC.pages[0].PageSize != shared.MaxPageSize {
		t.Fatalf("first page request = %+v", listUC.pages[0])
	}
	if listUC.pages[1].Page != 2 || listUC.pages[1].PageSize != shared.MaxPageSize {
		t.Fatalf("second page request = %+v", listUC.pages[1])
	}

	reader := csv.NewReader(strings.NewReader(recorder.Body.String()))
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	if len(rows) != 151 {
		t.Fatalf("expected 151 csv rows, got %d", len(rows))
	}

	first := rows[1]
	if first[4] != "-0.026500" {
		t.Fatalf("amount usd = %q, want %q", first[4], "-0.026500")
	}
	if first[5] != "-R$ 0,159" {
		t.Fatalf("amount brl = %q, want %q", first[5], "-R$ 0,159")
	}
	if first[7] != "R$ 193,885" {
		t.Fatalf("balance before brl = %q, want %q", first[7], "R$ 193,885")
	}
	if first[9] != "R$ 193,726" {
		t.Fatalf("balance after brl = %q, want %q", first[9], "R$ 193,726")
	}
	if first[10] != "ref-0" {
		t.Fatalf("reference id = %q, want %q", first[10], "ref-0")
	}
}

func buildExportTransactions(offset, count int) []*balancedomain.Transaction {
	items := make([]*balancedomain.Transaction, 0, count)
	for index := 0; index < count; index++ {
		globalIndex := offset + index
		createdAt := time.Date(2026, time.April, 9, 18, 50, 0, 0, time.UTC).Add(-time.Duration(globalIndex) * time.Minute)
		amount := int64(10_000 + globalIndex)
		balanceBefore := int64(32_000_000 - globalIndex*50)
		balanceAfter := balanceBefore - amount
		if globalIndex == 0 {
			amount = 26_500
			balanceBefore = 32_314_200
			balanceAfter = 32_287_700
		}
		formattedIndex := strconv.Itoa(globalIndex)
		refID := "ref-" + formattedIndex
		items = append(items, &balancedomain.Transaction{
			ID:                 "tx-" + formattedIndex,
			BalanceID:          "bal-1",
			WorkspaceID:        "ws-1",
			Type:               balancedomain.TransactionTypeDebit,
			Amount:             amount,
			ResourceType:       balancedomain.ResourceTypeMoney,
			BalanceBefore:      balanceBefore,
			BalanceAfter:       balanceAfter,
			ServiceType:        balancedomain.ServiceAI,
			ReferenceID:        &refID,
			Description:        "IA google/gemini-3.1-pro-preview",
			ExchangeRateMicros: 6_000_000,
			CreatedAt:          createdAt,
		})
	}
	return items
}
