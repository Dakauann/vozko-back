package report_renderers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	balancedomain "vozko/domain/balance"
	"vozko/domain/export"
	"vozko/domain/report"
	balance_usecase "vozko/usecases/balance"
)

type ConversationEntriesParams struct {
	Filter export.ExportFilter `json:"filter"`
	Label  string              `json:"label,omitempty"`
}

type ConversationEntriesRenderer struct {
	exporter export.ExportEntriesUseCase
}

func NewConversationEntriesRenderer(exporter export.ExportEntriesUseCase) *ConversationEntriesRenderer {
	return &ConversationEntriesRenderer{exporter: exporter}
}

func (r *ConversationEntriesRenderer) Kind() report.Kind {
	return report.KindConversationEntries
}

func (r *ConversationEntriesRenderer) Formats() []report.Format {
	return []report.Format{report.FormatCSV}
}

func (r *ConversationEntriesRenderer) Render(
	ctx context.Context,
	job report.Job,
	progress report.ProgressFunc,
) (report.Artifact, error) {
	if r.exporter == nil {
		return report.Artifact{}, report.ErrNoRenderer
	}

	var params ConversationEntriesParams
	if err := json.Unmarshal(job.Params, &params); err != nil {
		return report.Artifact{}, fmt.Errorf("entries report: reading parameters: %w", err)
	}

	filter := params.Filter
	filter.Scope.WorkspaceID = job.WorkspaceID

	progress(10)

	var buffer bytes.Buffer
	if _, err := buffer.WriteString(report.UTF8BOM); err != nil {
		return report.Artifact{}, err
	}

	rows, err := r.exporter.Export(ctx, filter, &buffer)
	if err != nil {
		return report.Artifact{}, err
	}
	if rows == 0 {
		return report.Artifact{}, report.ErrEmptyResult
	}

	progress(95)
	return report.Artifact{
		Data:        buffer.Bytes(),
		ContentType: report.FormatCSV.ContentType(),
		Filename:    report.Filename("csv", params.Label, string(filter.EntryType)),
		RowCount:    int64(rows),
	}, nil
}

type OpportunitiesParams struct {
	PipelineID             string   `json:"pipelineId"`
	DepartmentIDs          []string `json:"departmentIds,omitempty"`
	Restrict               bool     `json:"restrict,omitempty"`
	AssigneeOverrideUserID string   `json:"assigneeOverrideUserId,omitempty"`
	Label                  string   `json:"label,omitempty"`
}

type OpportunityExporter interface {
	Export(workspaceID, pipelineID string, departmentIDs []string, restrict bool, assigneeOverrideUserID string, w io.Writer) (int, error)
}

type OpportunitiesRenderer struct {
	exporter OpportunityExporter
}

func NewOpportunitiesRenderer(exporter OpportunityExporter) *OpportunitiesRenderer {
	return &OpportunitiesRenderer{exporter: exporter}
}

func (r *OpportunitiesRenderer) Kind() report.Kind { return report.KindOpportunities }

func (r *OpportunitiesRenderer) Formats() []report.Format {
	return []report.Format{report.FormatCSV}
}

func (r *OpportunitiesRenderer) Render(
	ctx context.Context,
	job report.Job,
	progress report.ProgressFunc,
) (report.Artifact, error) {
	if r.exporter == nil {
		return report.Artifact{}, report.ErrNoRenderer
	}
	var params OpportunitiesParams
	if err := json.Unmarshal(job.Params, &params); err != nil {
		return report.Artifact{}, fmt.Errorf("opportunities report: reading parameters: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return report.Artifact{}, err
	}

	progress(10)
	var buffer bytes.Buffer
	rows, err := r.exporter.Export(
		job.WorkspaceID,
		params.PipelineID,
		params.DepartmentIDs,
		params.Restrict,
		params.AssigneeOverrideUserID,
		&buffer,
	)
	if err != nil {
		return report.Artifact{}, err
	}

	progress(95)
	return report.Artifact{
		Data:        buffer.Bytes(),
		ContentType: report.FormatCSV.ContentType(),
		Filename:    report.Filename("csv", "oportunidades", params.Label),
		RowCount:    int64(rows),
	}, nil
}

type BalanceTransactionsParams struct {
	ServiceType string `json:"serviceType,omitempty"`
	Type        string `json:"type,omitempty"`
	StartDate   string `json:"startDate,omitempty"`
	EndDate     string `json:"endDate,omitempty"`
}

type BalanceTransactionsWriter interface {
	WriteCSV(input balancedomain.ListTransactionsInput, w io.Writer) (int, error)
	WriteXLSX(input balancedomain.ListTransactionsInput, w io.Writer) (int, error)
}

type BalanceTransactionsRenderer struct {
	exporter BalanceTransactionsWriter
}

func NewBalanceTransactionsRenderer(exporter BalanceTransactionsWriter) *BalanceTransactionsRenderer {
	return &BalanceTransactionsRenderer{exporter: exporter}
}

func (r *BalanceTransactionsRenderer) Kind() report.Kind {
	return report.KindBalanceTransactions
}

func (r *BalanceTransactionsRenderer) Formats() []report.Format {
	return []report.Format{report.FormatCSV, report.FormatXLSX}
}

func (r *BalanceTransactionsRenderer) Render(
	ctx context.Context,
	job report.Job,
	progress report.ProgressFunc,
) (report.Artifact, error) {
	if r.exporter == nil {
		return report.Artifact{}, report.ErrNoRenderer
	}
	var params BalanceTransactionsParams
	if err := json.Unmarshal(job.Params, &params); err != nil {
		return report.Artifact{}, fmt.Errorf("balance report: reading parameters: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return report.Artifact{}, err
	}

	input, err := balance_usecase.BuildTransactionsFilter(balance_usecase.TransactionsFilter{
		WorkspaceID: job.WorkspaceID,
		ServiceType: params.ServiceType,
		Type:        params.Type,
		StartDate:   params.StartDate,
		EndDate:     params.EndDate,
	})
	if err != nil {
		return report.Artifact{}, fmt.Errorf("balance report: %w", err)
	}

	progress(10)

	var buffer bytes.Buffer
	var rows int
	if job.Format == report.FormatXLSX {
		rows, err = r.exporter.WriteXLSX(input, &buffer)
	} else {
		rows, err = r.exporter.WriteCSV(input, &buffer)
	}
	if err != nil {
		return report.Artifact{}, err
	}

	progress(95)
	return report.Artifact{
		Data:        buffer.Bytes(),
		ContentType: job.Format.ContentType(),
		Filename:    report.Filename(string(job.Format), "transacoes", params.StartDate, params.EndDate),
		RowCount:    int64(rows),
	}, nil
}
