package advertising

import (
	"context"
	"errors"
	"strings"
	"time"

	"vozko/domain/report"
)

const (
	MaxReportExportBytes = 10 << 20
	KeptReportExports    = 100
)

var (
	ErrReportExportNotFound = errors.New("report export not found")
	ErrReportExportTooLarge = errors.New("report export is larger than the allowed size")
)

type ExportLabels struct {
	Object     string                          `json:"object"`
	Day        string                          `json:"day"`
	Total      string                          `json:"total"`
	Breakdowns map[Breakdown]string            `json:"breakdowns"`
	Values     map[Breakdown]map[string]string `json:"values"`
	Metrics    map[ReportMetric]string         `json:"metrics"`
}

func (l ExportLabels) breakdown(b Breakdown) string {
	if label := l.Breakdowns[b]; label != "" {
		return label
	}
	return string(b)
}

func (l ExportLabels) value(b Breakdown, v string) string {
	if label := l.Values[b][v]; label != "" {
		return label
	}
	return v
}

func (l ExportLabels) metric(m ReportMetric) string {
	if label := l.Metrics[m]; label != "" {
		return label
	}
	return string(m)
}

func (l ExportLabels) metricHeader(metrics []ReportMetric) []string {
	out := make([]string, 0, len(metrics))
	for _, m := range metrics {
		out = append(out, l.metric(m))
	}
	return out
}

func MoneyCSVCell(micros *int64) report.CSVCell {
	if micros == nil {
		return report.Empty()
	}
	return report.Number(MicrosToAmount(*micros))
}

func exportCell(metric ReportMetric, value *float64) report.CSVCell {
	if value == nil {
		return report.Empty()
	}
	switch metric.Kind() {
	case KindMoney:
		micros := int64(*value)
		return MoneyCSVCell(&micros)
	case KindCount:
		return report.Int(int64(*value))
	}
	return report.Number(*value)
}

func exportCells(metrics []ReportMetric, values ReportCells) []report.CSVCell {
	out := make([]report.CSVCell, 0, len(metrics))
	for _, m := range metrics {
		out = append(out, exportCell(m, values[m]))
	}
	return out
}

func (t ReportTable) CSVSection(labels ExportLabels) report.CSVSection {
	byObject := t.View == ViewPivot || len(t.Breakdowns) == 0
	header := []string{}
	if byObject {
		header = append(header, labels.Object)
	}
	for _, b := range t.Breakdowns {
		header = append(header, labels.breakdown(b))
	}
	header = append(header, labels.metricHeader(t.Metrics)...)
	rows := make([][]report.CSVCell, 0, len(t.Rows)+1)
	for _, row := range t.Rows {
		cells := []report.CSVCell{}
		if byObject {
			cells = append(cells, report.Text(row.Name))
		}
		for i, v := range row.Dimensions {
			cells = append(cells, report.Text(labels.value(t.Breakdowns[i], v)))
		}
		rows = append(rows, append(cells, exportCells(t.Metrics, row.Values)...))
	}
	total := []report.CSVCell{report.Text(labels.Total)}
	for range len(header) - len(t.Metrics) - 1 {
		total = append(total, report.Empty())
	}
	rows = append(rows, append(total, exportCells(t.Metrics, t.Totals)...))
	return report.CSVSection{Header: header, Rows: rows}
}

func SeriesCSVSection(metrics []ReportMetric, series []ReportDay, labels ExportLabels) report.CSVSection {
	rows := make([][]report.CSVCell, 0, len(series))
	for _, day := range series {
		rows = append(rows, append([]report.CSVCell{report.Text(day.Day.Format(DayLayout))}, exportCells(metrics, day.Values)...))
	}
	return report.CSVSection{Header: append([]string{labels.Day}, labels.metricHeader(metrics)...), Rows: rows}
}

type ReportExport struct {
	ID          string
	WorkspaceID string
	AdAccountID string
	ReportID    string
	Name        string
	Range       DateRange
	Rows        int
	SizeBytes   int
	Content     []byte
	CreatedBy   string
	CreatedAt   time.Time
}

func NewReportExport(workspaceID, accountID, reportID, userID, name string, r DateRange, rows int, content []byte) (*ReportExport, error) {
	name = strings.TrimSpace(name)
	v := newIssues()
	v.text("name", name, true, maxNameRunes)
	if err := v.err(); err != nil {
		return nil, err
	}
	if len(content) > MaxReportExportBytes {
		return nil, ErrReportExportTooLarge
	}
	return &ReportExport{
		WorkspaceID: workspaceID, AdAccountID: accountID, ReportID: reportID, CreatedBy: userID,
		Name: name, Range: r, Rows: rows, SizeBytes: len(content), Content: content,
	}, nil
}

type ReportExportRepository interface {
	Create(ctx context.Context, e *ReportExport) error
	Find(ctx context.Context, workspaceID, id string) (*ReportExport, error)
	List(ctx context.Context, workspaceID string, limit int) ([]*ReportExport, error)
	Delete(ctx context.Context, workspaceID, id string) error
	KeepNewest(ctx context.Context, workspaceID string, keep int) error
}
