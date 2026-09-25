package copilottools

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"vozko/domain/attendance"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workspace"
)

const (
	dispatchDaily     = "daily"
	dispatchFailures  = "failures"
	dispatchTags      = "tags"
	dispatchCampaigns = "campaigns"
)

type campaignDispatchTool struct {
	reports wc.GetDispatchReportUseCase
	deps    AttendanceDeps
}

func NewCampaignDispatchTool(reports wc.GetDispatchReportUseCase, deps AttendanceDeps) copilot.Tool {
	return &campaignDispatchTool{reports: reports, deps: deps}
}

func (t *campaignDispatchTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceWhatsAppCampaigns, Action: workspace.ActionRead}
}

func (t *campaignDispatchTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "campaign_dispatch",
		Description: fmt.Sprintf("Resultado dos disparos de campanha (o bloco \"Disparo da campanha\" da página de atendimento): "+
			"base de contatos, enviadas, entregues, lidas, respostas e falhas. Sem campaign_id cobre as campanhas com contatos "+
			"no período; com campaign_id, a campanha inteira. Em details peça daily (evolução por dia, vira dataset), failures "+
			"(motivos de falha), tags (etiquetas mais usadas) e campaigns (campanhas do período; só sem campaign_id). "+
			"Período de até %d dias, contado no fuso do workspace.", wce.MaxReportDays),
		Parameters: map[string]tools.Parameter{
			"date_from":     {Type: "string", Description: "início YYYY-MM-DD; omita para o período da tela (ou os últimos 30 dias)"},
			"date_to":       {Type: "string", Description: "fim YYYY-MM-DD (inclusivo)"},
			"campaign_id":   {Type: "string", Description: "id da campanha (de details campaigns); omita para a da tela; \"all\" para todas"},
			"department_id": {Type: "string", Description: "departamento, só sem campaign_id; omita para o da tela; \"all\" para todos que o usuário vê"},
			"details":       {Type: "array", Description: "blocos extras: daily, failures, tags, campaigns", Items: &tools.ParameterItems{Type: "string"}},
		},
	}
}

func (t *campaignDispatchTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	window, err := attendance.ParseWindow(
		onScreen(argString(args, "date_from"), cc.View.DateFrom),
		onScreen(argString(args, "date_to"), cc.View.DateTo),
		t.deps.Now(),
	)
	if err != nil {
		return dispatchFailure(err)
	}
	period := wc.ReportPeriod{DateFrom: window.From.Format(attendance.DayLayout), DateTo: window.To.Format(attendance.DayLayout)}
	access, err := t.access(cc, args)
	if err != nil {
		return dispatchFailure(err)
	}

	summary, err := t.reports.Summary(ctx, access, period)
	if err != nil {
		return dispatchFailure(err)
	}
	data := map[string]interface{}{
		"query": map[string]interface{}{
			"date_from":     period.DateFrom,
			"date_to":       period.DateTo,
			"campaign_id":   orAll(access.CampaignID),
			"department_id": departmentLabel(access),
		},
		"summary": summary,
	}
	for _, detail := range argStringList(args, "details") {
		if err := t.detail(ctx, cc, detail, access, period, data); err != nil {
			return dispatchFailure(err)
		}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: data}
}

var errUnknownDispatchDetail = errors.New("detail inválido: use daily, failures, tags ou campaigns")

func (t *campaignDispatchTool) access(cc copilot.Context, args map[string]interface{}) (wc.ReportAccess, error) {
	access := wc.ReportAccess{WorkspaceID: cc.WorkspaceID, AllowDepartment: cc.Departments.Allows}
	campaign := onScreen(argString(args, "campaign_id"), cc.View.CampaignID)
	if campaign != "" {
		if _, err := uuid.Parse(campaign); err != nil {
			return wc.ReportAccess{}, errUnknownCampaign
		}
		access.CampaignID = campaign
		return access, nil
	}
	department := onScreen(argString(args, "department_id"), cc.View.DepartmentID)
	if department == "" {
		access.DepartmentIDs, access.DepartmentsBlocked = cc.Departments.ListScope()
		return access, nil
	}
	if !cc.Departments.Allows(department) {
		return wc.ReportAccess{}, wc.ErrCampaignAccessDenied
	}
	if err := t.deps.checkDepartment(cc.WorkspaceID, department); err != nil {
		return wc.ReportAccess{}, err
	}
	access.DepartmentIDs = []string{department}
	return access, nil
}

func (t *campaignDispatchTool) detail(ctx context.Context, cc copilot.Context, detail string, access wc.ReportAccess, period wc.ReportPeriod, data map[string]interface{}) error {
	switch detail {
	case dispatchDaily:
		daily, err := t.reports.Daily(ctx, access, period)
		if err != nil {
			return err
		}
		dataset, err := dailyDataset(daily.Days)
		if err != nil {
			return err
		}
		data[dispatchDaily] = map[string]interface{}{"timezone": daily.Timezone, "dataset": keep(cc, dataset).Preview()}
	case dispatchFailures:
		failures, err := t.reports.Failures(ctx, access, period)
		if err != nil {
			return err
		}
		data[dispatchFailures] = failures.Reasons
	case dispatchTags:
		tags, err := t.reports.Tags(ctx, access, period)
		if err != nil {
			return err
		}
		data[dispatchTags] = tags.Tags
	case dispatchCampaigns:
		if access.CampaignID != "" {
			return nil
		}
		campaigns, err := t.reports.Campaigns(ctx, access, period)
		if err != nil {
			return err
		}
		dataset, err := campaignsDataset(campaigns.Campaigns)
		if err != nil {
			return err
		}
		data[dispatchCampaigns] = map[string]interface{}{
			"top":     largestCampaigns(campaigns.Campaigns),
			"total":   len(campaigns.Campaigns),
			"dataset": keep(cc, dataset).Handle(),
		}
	default:
		return errUnknownDispatchDetail
	}
	return nil
}

func largestCampaigns(rows []wce.CampaignFunnel) []wce.CampaignFunnel {
	ranked := append([]wce.CampaignFunnel(nil), rows...)
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].Base > ranked[j].Base })
	if len(ranked) > attendance.MaxAssistantListItems {
		ranked = ranked[:attendance.MaxAssistantListItems]
	}
	if ranked == nil {
		return []wce.CampaignFunnel{}
	}
	return ranked
}

func dailyDataset(days []wce.DayCount) (*copilot.Dataset, error) {
	d := copilot.NewDataset("campaign dispatch by day", []copilot.Column{
		{Key: "day", Label: "day", Kind: copilot.ColumnDate},
		{Key: "sent", Label: "sent", Kind: copilot.ColumnNumber},
		{Key: "delivered", Label: "delivered", Kind: copilot.ColumnNumber},
		{Key: "read", Label: "read", Kind: copilot.ColumnNumber},
		{Key: "replied", Label: "replied", Kind: copilot.ColumnNumber},
	})
	for _, day := range days {
		if err := d.AddRow(day.Day, day.Sent, day.Delivered, day.Read, day.Replied); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func campaignsDataset(rows []wce.CampaignFunnel) (*copilot.Dataset, error) {
	d := copilot.NewDataset("campaigns in the period", []copilot.Column{
		{Key: "campaign", Label: "campaign", Kind: copilot.ColumnText},
		{Key: "base", Label: "base", Kind: copilot.ColumnNumber},
		{Key: "sent", Label: "sent", Kind: copilot.ColumnNumber},
		{Key: "delivered", Label: "delivered", Kind: copilot.ColumnNumber},
		{Key: "read", Label: "read", Kind: copilot.ColumnNumber},
		{Key: "replied", Label: "replied", Kind: copilot.ColumnNumber},
		{Key: "failed", Label: "failed", Kind: copilot.ColumnNumber},
	})
	for _, r := range rows {
		if err := d.AddRow(r.CampaignName, r.Base, r.Sent, r.Delivered, r.Read, r.Replied, r.Failed); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func departmentLabel(access wc.ReportAccess) string {
	if len(access.DepartmentIDs) == 1 {
		return access.DepartmentIDs[0]
	}
	return allScope
}

func dispatchFailure(err error) copilot.Result {
	switch {
	case errors.Is(err, wc.ErrCampaignNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "campanha não encontrada neste workspace"}
	case errors.Is(err, wc.ErrCampaignAccessDenied):
		return copilot.Result{Status: copilot.StatusDenied, Message: "sem acesso a esta campanha ou departamento"}
	case errors.Is(err, wce.ErrReportWindowTooLong):
		return copilot.Result{Status: copilot.StatusError, Message: fmt.Sprintf("o relatório de disparos cobre até %d dias por consulta; divida o período", wce.MaxReportDays)}
	case errors.Is(err, wce.ErrReportWindowInvalid):
		return copilot.Result{Status: copilot.StatusError, Message: "período inválido: use date_from e date_to em YYYY-MM-DD"}
	case errors.Is(err, errUnknownDispatchDetail):
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	return analyticsFailure("campaign_dispatch", err)
}
