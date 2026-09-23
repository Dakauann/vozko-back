package balance_usecase

import (
	"encoding/csv"
	"fmt"
	"io"

	"github.com/xuri/excelize/v2"

	balancedomain "vozko/domain/balance"
	"vozko/domain/shared"
	"vozko/domain/workspace/workspace_pricing"
)

const transactionExportPageSize = shared.MaxPageSize

type TransactionLister interface {
	Execute(input balancedomain.ListTransactionsInput) (*shared.PaginatedResult[*balancedomain.Transaction], error)
}

type ExchangeRateReader interface {
	Execute() (*workspace_pricing.PricingItem, error)
}

type TransactionsExporter struct {
	transactions TransactionLister
	exchangeRate ExchangeRateReader
}

func NewTransactionsExporter(
	transactions TransactionLister,
	exchangeRate ExchangeRateReader,
) *TransactionsExporter {
	return &TransactionsExporter{transactions: transactions, exchangeRate: exchangeRate}
}

func TransactionExportHeaders() []string {
	return []string{
		"Date", "Type", "Service", "Description",
		"Amount (USD)", "Amount (BRL)",
		"Balance Before (USD)", "Balance Before (BRL)",
		"Balance After (USD)", "Balance After (BRL)",
		"Reference ID",
	}
}

func (e *TransactionsExporter) Rows(input balancedomain.ListTransactionsInput) ([][]string, error) {
	if e == nil || e.transactions == nil {
		return nil, fmt.Errorf("balance export: the transaction source is not configured")
	}

	var all []*balancedomain.Transaction
	for page := 1; ; page++ {
		input.QueryOptions = shared.QueryOptions{
			Pagination: shared.Pagination{Page: page, PageSize: transactionExportPageSize},
		}
		result, err := e.transactions.Execute(input)
		if err != nil {
			return nil, err
		}
		if len(result.Items) == 0 {
			break
		}
		all = append(all, result.Items...)
		if int64(len(all)) >= result.TotalItems {
			break
		}
		if len(result.Items) < result.PageSize {
			break
		}
	}

	currentRate := 0.0
	if e.exchangeRate != nil {
		if item, err := e.exchangeRate.Execute(); err == nil && item != nil && item.PriceMicros > 0 {
			currentRate = float64(item.PriceMicros) / 1_000_000
		}
	}

	rows := make([][]string, 0, len(all))
	for _, t := range all {
		refID := ""
		if t.ReferenceID != nil {
			refID = *t.ReferenceID
		}
		signed := t.Amount
		if t.Type == balancedomain.TransactionTypeDebit {
			signed = -signed
		}
		txRate := float64(t.ExchangeRateMicros) / 1_000_000
		if t.ExchangeRateMicros <= 0 {
			txRate = currentRate
		}
		rows = append(rows, []string{
			t.CreatedAt.Format("2006-01-02 15:04:05"),
			string(t.Type),
			string(t.ServiceType),
			t.Description,
			shared.FormatMicrosUSD(signed),
			shared.FormatMicrosBRL(signed, txRate),
			shared.FormatMicrosUSD(t.BalanceBefore),
			shared.FormatMicrosBRL(t.BalanceBefore, currentRate),
			shared.FormatMicrosUSD(t.BalanceAfter),
			shared.FormatMicrosBRL(t.BalanceAfter, currentRate),
			refID,
		})
	}
	return rows, nil
}

func (e *TransactionsExporter) WriteCSV(input balancedomain.ListTransactionsInput, w io.Writer) (int, error) {
	rows, err := e.Rows(input)
	if err != nil {
		return 0, err
	}
	if err := WriteTransactionsCSV(rows, w); err != nil {
		return 0, err
	}
	return len(rows), nil
}

func (e *TransactionsExporter) WriteXLSX(input balancedomain.ListTransactionsInput, w io.Writer) (int, error) {
	rows, err := e.Rows(input)
	if err != nil {
		return 0, err
	}
	if err := WriteTransactionsXLSX(rows, w); err != nil {
		return 0, err
	}
	return len(rows), nil
}

func WriteTransactionsCSV(rows [][]string, w io.Writer) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(TransactionExportHeaders()); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func WriteTransactionsXLSX(rows [][]string, w io.Writer) error {
	file := excelize.NewFile()
	sheet := "Transactions"
	index, err := file.NewSheet(sheet)
	if err != nil {
		return err
	}
	file.SetActiveSheet(index)
	_ = file.DeleteSheet("Sheet1")

	for column, header := range TransactionExportHeaders() {
		cell, err := excelize.CoordinatesToCellName(column+1, 1)
		if err != nil {
			return err
		}
		if err := file.SetCellValue(sheet, cell, header); err != nil {
			return err
		}
	}
	for rowIndex, row := range rows {
		for column, value := range row {
			cell, err := excelize.CoordinatesToCellName(column+1, rowIndex+2)
			if err != nil {
				return err
			}
			if err := file.SetCellValue(sheet, cell, value); err != nil {
				return err
			}
		}
	}
	return file.Write(w)
}
