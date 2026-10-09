package report_renderers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	balancedomain "vozko/domain/balance"
	"vozko/domain/export"
	"vozko/domain/opportunity"
	"vozko/domain/report"
	"vozko/domain/shared"
	"vozko/domain/workspace"
	dept "vozko/domain/workspace/workspace_department"
	balance_usecase "vozko/usecases/balance"
)

type DepartmentScopes interface {
	For(ctx context.Context, workspaceID, userID string, isAdmin bool) (*dept.DepartmentFilter, error)
}

type DealScopes interface {
	Scope(by shared.Person, workspaceID string) (opportunity.DealScope, error)
}

var entryResources = map[export.EntryType]workspace.Resource{
	export.EntryTypeWhatsApp:           workspace.ResourceWhatsAppCampaigns,
	export.EntryTypeInstagram:          workspace.ResourceInstagramAccounts,
	export.EntryTypeTelegram:           workspace.ResourceTelegramAccounts,
	export.EntryTypeFacebook:           workspace.ResourceFacebookPages,
	export.EntryTypeUnofficialWhatsApp: workspace.ResourceUnofficialWhatsAppCampaigns,
}

func readPolicy(resource workspace.Resource) report.Policy {
	return report.Policy{Required: []workspace.PermissionEntry{{Resource: resource, Action: workspace.ActionRead}}}
}

func requesterScopedPolicy(resource workspace.Resource) report.Policy {
	policy := readPolicy(resource)
	policy.RequesterOnly = true
	return policy
}

type ConversationEntriesParams struct {
	Filter export.ExportFilter `json:"filter"`
	Label  string              `json:"label,omitempty"`
}

type ConversationEntriesRenderer struct {
	exporter export.ExportEntriesUseCase
	scopes   DepartmentScopes
}

func NewConversationEntriesRenderer(exporter export.ExportEntriesUseCase, scopes DepartmentScopes) *ConversationEntriesRenderer {
	return &ConversationEntriesRenderer{exporter: exporter, scopes: scopes}
}

func (r *ConversationEntriesRenderer) Policy(job report.Job) (report.Policy, error) {
	var params ConversationEntriesParams
	if err := json.Unmarshal(job.Params, &params); err != nil {
		return report.Policy{}, fmt.Errorf("%w: %v", report.ErrNoPolicy, err)
	}
	resource, ok := entryResources[params.Filter.EntryType]
	if !ok {
		return report.Policy{}, fmt.Errorf("%w: entries of %q", report.ErrNoPolicy, params.Filter.EntryType)
	}
	return requesterScopedPolicy(resource), nil
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
	if r.exporter == nil || r.scopes == nil {
		return report.Artifact{}, report.ErrNoRenderer
	}

	var params ConversationEntriesParams
	if err := json.Unmarshal(job.Params, &params); err != nil {
		return report.Artifact{}, fmt.Errorf("entries report: reading parameters: %w", err)
	}

	scope, err := r.scopes.For(ctx, job.WorkspaceID, job.RequestedBy, job.RequestedByAdmin)
	if err != nil {
		return report.Artifact{}, fmt.Errorf("entries report: the department scope of the requester: %w", err)
	}
	departments, none := scope.Narrow(params.Filter.Scope.DepartmentIDs)
	if none {
		return report.Artifact{}, report.ErrEmptyResult
	}

	filter := params.Filter
	filter.Scope.WorkspaceID = job.WorkspaceID
	filter.Scope.DepartmentIDs = departments

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
	PipelineID string `json:"pipelineId"`
	Label      string `json:"label,omitempty"`
}

type OpportunityExporter interface {
	Export(workspaceID, pipelineID string, departmentIDs []string, restrict bool, assigneeOverrideUserID string, w io.Writer) (int, error)
}

type OpportunitiesRenderer struct {
	exporter OpportunityExporter
	scopes   DealScopes
}

func NewOpportunitiesRenderer(exporter OpportunityExporter, scopes DealScopes) *OpportunitiesRenderer {
	return &OpportunitiesRenderer{exporter: exporter, scopes: scopes}
}

func (r *OpportunitiesRenderer) Policy(report.Job) (report.Policy, error) {
	return requesterScopedPolicy(workspace.ResourceConversations), nil
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
	if r.exporter == nil || r.scopes == nil {
		return report.Artifact{}, report.ErrNoRenderer
	}
	var params OpportunitiesParams
	if err := json.Unmarshal(job.Params, &params); err != nil {
		return report.Artifact{}, fmt.Errorf("opportunities report: reading parameters: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return report.Artifact{}, err
	}

	scope, err := r.scopes.Scope(shared.Person{UserID: job.RequestedBy, SystemAdmin: job.RequestedByAdmin}, job.WorkspaceID)
	if err != nil {
		return report.Artifact{}, fmt.Errorf("opportunities report: the scope of the requester: %w", err)
	}

	progress(10)
	var buffer bytes.Buffer
	rows, err := r.exporter.Export(
		job.WorkspaceID,
		params.PipelineID,
		scope.DepartmentIDs,
		scope.Restrict,
		scope.AssigneeOverride,
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

func (r *BalanceTransactionsRenderer) Policy(report.Job) (report.Policy, error) {
	return readPolicy(workspace.ResourceBalance), nil
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
