package report_renderers

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"vozko/domain/report"
)

const maxPrintRows = 2000

type TabularPrintTable struct {
	Columns   []string   `json:"columns"`
	Rows      [][]string `json:"rows"`
	Total     int        `json:"total"`
	Truncated bool       `json:"truncated"`
}

type csvProducer interface {
	Render(ctx context.Context, job report.Job, progress report.ProgressFunc) (report.Artifact, error)
}

func tabularPrintData(
	ctx context.Context,
	producer csvProducer,
	job report.Job,
) (interface{}, error) {
	csvJob := job
	csvJob.Format = report.FormatCSV

	artifact, err := producer.Render(ctx, csvJob, func(int) {})
	if err != nil {
		return nil, err
	}

	table, err := parseCSVTable(artifact.Data)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"table": table}, nil
}

func parseCSVTable(data []byte) (*TabularPrintTable, error) {
	body := strings.TrimPrefix(string(data), report.UTF8BOM)

	reader := csv.NewReader(strings.NewReader(body))
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err == io.EOF {
		return &TabularPrintTable{Columns: []string{}, Rows: [][]string{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("print table: reading the header: %w", err)
	}

	table := &TabularPrintTable{Columns: header, Rows: [][]string{}}
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("print table: reading a row: %w", err)
		}
		table.Total++
		if len(table.Rows) < maxPrintRows {
			table.Rows = append(table.Rows, record)
		}
	}
	table.Truncated = table.Total > len(table.Rows)
	return table, nil
}

func (r *ConversationEntriesRenderer) PrintData(ctx context.Context, job report.Job) (interface{}, error) {
	return tabularPrintData(ctx, r, job)
}

func (r *OpportunitiesRenderer) PrintData(ctx context.Context, job report.Job) (interface{}, error) {
	return tabularPrintData(ctx, r, job)
}

func (r *BalanceTransactionsRenderer) PrintData(ctx context.Context, job report.Job) (interface{}, error) {
	return tabularPrintData(ctx, r, job)
}
