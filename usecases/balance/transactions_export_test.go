package balance_usecase

import (
	"bytes"
	"encoding/csv"
	"strconv"
	"testing"
	"time"

	balancedomain "vozko/domain/balance"
	"vozko/domain/shared"
	"vozko/domain/workspace/workspace_pricing"
)

type listStub struct {
	t       *testing.T
	pages   []shared.Pagination
	results map[int]*shared.PaginatedResult[*balancedomain.Transaction]
}

func (s *listStub) Execute(input balancedomain.ListTransactionsInput) (*shared.PaginatedResult[*balancedomain.Transaction], error) {
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

type rateStub struct{}

func (rateStub) Execute() (*workspace_pricing.PricingItem, error) {
	return &workspace_pricing.PricingItem{PriceMicros: 6_000_000, Currency: "BRL"}, nil
}

func buildTransactions(offset, count int) []*balancedomain.Transaction {
	items := make([]*balancedomain.Transaction, 0, count)
	for index := 0; index < count; index++ {
		globalIndex := offset + index
		createdAt := time.Date(2026, time.April, 9, 18, 50, 0, 0, time.UTC).
			Add(-time.Duration(globalIndex) * time.Minute)
		amount := int64(10_000 + globalIndex)
		balanceBefore := int64(32_000_000 - globalIndex*50)
		balanceAfter := balanceBefore - amount
		if globalIndex == 0 {
			amount = 26_500
			balanceBefore = 32_314_200
			balanceAfter = 32_287_700
		}
		reference := "ref-" + strconv.Itoa(globalIndex)
		items = append(items, &balancedomain.Transaction{
			ID:                 "tx-" + strconv.Itoa(globalIndex),
			BalanceID:          "bal-1",
			WorkspaceID:        "ws-1",
			Type:               balancedomain.TransactionTypeDebit,
			Amount:             amount,
			ResourceType:       balancedomain.ResourceTypeMoney,
			BalanceBefore:      balanceBefore,
			BalanceAfter:       balanceAfter,
			ServiceType:        balancedomain.ServiceAI,
			ReferenceID:        &reference,
			Description:        "IA google/gemini-3.1-pro-preview",
			ExchangeRateMicros: 6_000_000,
			CreatedAt:          createdAt,
		})
	}
	return items
}

func exporterWithTwoPages(t *testing.T) (*TransactionsExporter, *listStub) {
	t.Helper()
	lister := &listStub{
		t: t,
		results: map[int]*shared.PaginatedResult[*balancedomain.Transaction]{
			1: {Items: buildTransactions(0, 100), Page: 1, PageSize: shared.MaxPageSize, TotalItems: 150, TotalPages: 1},
			2: {Items: buildTransactions(100, 50), Page: 2, PageSize: shared.MaxPageSize, TotalItems: 150, TotalPages: 1},
		},
	}
	return NewTransactionsExporter(lister, rateStub{}), lister
}

func TestExporterPaginatesPastTheReportedFirstPage(t *testing.T) {
	exporter, lister := exporterWithTwoPages(t)

	rows, err := exporter.Rows(balancedomain.ListTransactionsInput{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("rows: %v", err)
	}

	if len(rows) != 150 {
		t.Fatalf("got %d rows, want every transaction in the period", len(rows))
	}
	if len(lister.pages) != 2 {
		t.Fatalf("asked for %d pages, want 2", len(lister.pages))
	}
	if lister.pages[0].PageSize != shared.MaxPageSize {
		t.Fatalf("page size = %d", lister.pages[0].PageSize)
	}
}

func TestExporterFormatsMoneyForBrazil(t *testing.T) {
	exporter, _ := exporterWithTwoPages(t)

	var buffer bytes.Buffer
	count, err := exporter.WriteCSV(balancedomain.ListTransactionsInput{WorkspaceID: "ws-1"}, &buffer)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if count != 150 {
		t.Fatalf("wrote %d rows", count)
	}

	records, err := csv.NewReader(bytes.NewReader(buffer.Bytes())).ReadAll()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(records) != 151 {
		t.Fatalf("csv has %d lines, want a header plus 150 rows", len(records))
	}

	first := records[1]
	for index, want := range map[int]string{
		4:  "-0.026500",
		5:  "-R$ 0,159",
		7:  "R$ 193,885",
		9:  "R$ 193,726",
		10: "ref-0",
	} {
		if first[index] != want {
			t.Fatalf("column %d = %q, want %q", index, first[index], want)
		}
	}
}

func TestExporterWritesAReadableXLSX(t *testing.T) {
	exporter, _ := exporterWithTwoPages(t)

	var buffer bytes.Buffer
	count, err := exporter.WriteXLSX(balancedomain.ListTransactionsInput{WorkspaceID: "ws-1"}, &buffer)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if count != 150 {
		t.Fatalf("wrote %d rows", count)
	}
	if !bytes.HasPrefix(buffer.Bytes(), []byte("PK")) {
		t.Fatal("the xlsx is not a zip container")
	}
}
