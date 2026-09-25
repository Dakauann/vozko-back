package campaignreport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/infra/http/middleware"
)

type Handler struct {
	reports wc.GetDispatchReportUseCase
}

func NewHandler(reports wc.GetDispatchReportUseCase) *Handler {
	return &Handler{reports: reports}
}

// @Summary		Seção do relatório de disparos na visão de atendimento
// @Description	Retorna uma seção do relatório de disparos de campanhas (summary, daily, failures, tags ou campaigns) para o painel de atendimento. Os dias são contados no fuso do horário de funcionamento do workspace (ou do departamento), como as demais seções de atendimento. Sem campaign_id, cobre as campanhas de disparo do período; com campaign_id, a campanha inteira. As seções ficam em cache por 60 segundos; quando o limite de consultas analíticas simultâneas está ocupado, responde 503 com Retry-After.
// @Tags			Atendimento
// @Produce		json
// @Param			section			path	string	true	"Seção"	Enums(summary, daily, failures, tags, campaigns)
// @Param			date_from		query	string	true	"Data inicial (YYYY-MM-DD)"
// @Param			date_to			query	string	true	"Data final (YYYY-MM-DD), no máximo 92 dias após a inicial"
// @Param			campaign_id		query	string	false	"ID da campanha"
// @Param			department_id	query	string	false	"ID do departamento (sem campaign_id)"
// @Success		200	{object}	whatsapp_campaign.ReportSummary
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Failure		504	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/attendance/campaigns/{section} [get]
func (h *Handler) GetSection(w http.ResponseWriter, r *http.Request) {
	section, ok := wc.ParseReportSection(mux.Vars(r)["section"])
	if !ok {
		response.WriteError(w, http.StatusNotFound, "unknown dispatch report section", nil)
		return
	}
	query := r.URL.Query()
	departmentIDs, blocked := requestedDepartments(r)
	access := wc.ReportAccess{
		WorkspaceID:        middleware.GetWorkspaceID(r),
		CampaignID:         strings.TrimSpace(query.Get("campaign_id")),
		DepartmentIDs:      departmentIDs,
		DepartmentsBlocked: blocked,
		AllowDepartment: func(departmentID string) bool {
			return httpx.CanAccessDepartment(r, departmentID)
		},
	}
	period := wc.ReportPeriod{
		DateFrom: strings.TrimSpace(query.Get("date_from")),
		DateTo:   strings.TrimSpace(query.Get("date_to")),
	}

	out, err := h.readSection(r.Context(), section, access, period)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func (h *Handler) readSection(ctx context.Context, section wc.ReportSection, access wc.ReportAccess, period wc.ReportPeriod) (any, error) {
	switch section {
	case wc.ReportSectionSummary:
		return h.reports.Summary(ctx, access, period)
	case wc.ReportSectionDaily:
		return h.reports.Daily(ctx, access, period)
	case wc.ReportSectionFailures:
		return h.reports.Failures(ctx, access, period)
	case wc.ReportSectionTags:
		return h.reports.Tags(ctx, access, period)
	case wc.ReportSectionCampaigns:
		return h.reports.Campaigns(ctx, access, period)
	}
	return nil, errUnknownSection
}

var errUnknownSection = errors.New("unknown dispatch report section")

func writeError(w http.ResponseWriter, err error) {
	if httpx.WriteAnalyticsLimit(w, err, "dispatch report") {
		return
	}
	switch {
	case errors.Is(err, errUnknownSection):
		response.WriteError(w, http.StatusNotFound, "unknown dispatch report section", nil)
	case errors.Is(err, wc.ErrCampaignNotFound):
		response.WriteError(w, http.StatusNotFound, "WhatsApp campaign not found", nil)
	case errors.Is(err, wc.ErrCampaignAccessDenied):
		response.WriteError(w, http.StatusForbidden, "You don't have access to this campaign", nil)
	case errors.Is(err, wce.ErrReportWindowInvalid):
		response.WriteValidationError(w, map[string]string{"date_from,date_to": "days as YYYY-MM-DD, date_from on or before date_to"})
	case errors.Is(err, wce.ErrReportWindowTooLong):
		response.WriteValidationError(w, map[string]string{"date_from,date_to": fmt.Sprintf("at most %d days", wce.MaxReportDays)})
	case errors.Is(err, wce.ErrReportScopeInvalid):
		response.WriteValidationError(w, map[string]string{"section": "the campaign breakdown needs the all-campaigns view"})
	default:
		response.WriteError(w, http.StatusInternalServerError, "Failed to load the dispatch report", nil)
	}
}

func requestedDepartments(r *http.Request) ([]string, bool) {
	requested := strings.TrimSpace(r.URL.Query().Get("department_id"))
	if requested == "" {
		return httpx.DepartmentFilterIDs(r), httpx.ShouldReturnEmptyDepartmentList(r)
	}
	if !httpx.CanAccessDepartment(r, requested) {
		return nil, true
	}
	return []string{requested}, false
}
