package export

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/campaign"
	exportdomain "vozko/domain/export"
	reportdomain "vozko/domain/report"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	whatsappcampaign_usecase "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/infra/http/middleware"
	report_usecase "vozko/usecases/report"
	report_renderers "vozko/usecases/report/renderers"
)

type ExportHandler struct {
	getWCUC whatsappcampaign_usecase.GetCampaignUseCase
	reports *report_usecase.Service
}

func NewExportHandler(
	getWCUC whatsappcampaign_usecase.GetCampaignUseCase,
	reports *report_usecase.Service,
) *ExportHandler {
	return &ExportHandler{getWCUC: getWCUC, reports: reports}
}

// @Summary		Exportar entradas de campanha do WhatsApp (CSV)
// @Description	Coloca na fila a exportacao em CSV das entradas (contatos) de uma campanha do WhatsApp do workspace, aplicando os filtros informados na query. O parâmetro status aceita múltiplos valores, separados por vírgula ou repetidos. O arquivo inclui BOM UTF-8 para abertura correta no Excel.
// @Tags			Campanhas do WhatsApp
// @Produce		json
// @Param			id						path	string	true	"Identificador da campanha"
// @Param			status					query	string	false	"Filtrar por status da entrada (aceita lista: SENT,DELIVERED,READ)"
// @Param			stageId					query	string	false	"Filtrar por etapa"
// @Param			search					query	string	false	"Buscar por número"
// @Param			interest				query	string	false	"Filtrar por interesse"
// @Param			disposition				query	string	false	"Filtrar por disposição"
// @Param			sentiment				query	string	false	"Filtrar por sentimento"
// @Param			qualification			query	string	false	"Filtrar por qualificação"
// @Param			nextAction				query	string	false	"Filtrar por próxima ação"
// @Param			hasAnalysis				query	bool	false	"Filtrar por presença de análise"
// @Param			attendanceQualityMin	query	int		false	"Qualidade de atendimento mínima"
// @Param			attendanceQualityMax	query	int		false	"Qualidade de atendimento máxima"
// @Success		202	{object}	report.Job	"Relatorio na fila; acompanhe por /reports/{id}"
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/campaigns/{id}/entries/export [get]
func (h *ExportHandler) ExportWhatsAppEntries(w http.ResponseWriter, r *http.Request) {
	req := ExportEntriesRequest{CampaignID: mux.Vars(r)["id"]}
	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	camp, err := h.getWCUC.Execute(req.CampaignID)
	if err != nil {
		response.WriteError(w, http.StatusNotFound, "Campaign not found", nil)
		return
	}
	if camp.WorkspaceID != middleware.GetWorkspaceID(r) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this campaign", nil)
		return
	}
	if !httpx.CanAccessDepartment(r, camp.DepartmentID) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this campaign", nil)
		return
	}

	filter, errs := h.parseExportFilter(r, req.CampaignID, exportdomain.EntryTypeWhatsApp)
	if errs != nil {
		response.WriteValidationError(w, errs)
		return
	}
	h.queueExport(w, r, filter, fmt.Sprintf("whatsapp-campaign-%s", req.CampaignID))
}

// @Summary		Exportar leads dos disparos do WhatsApp (CSV)
// @Description	Coloca na fila a exportacao em CSV dos leads de TODAS as campanhas do workspace de uma só vez, no mesmo recorte do resumo de disparos (período de criação da campanha, tipo e departamento). Use status para escolher os envios desejados — por exemplo status=SENT,DELIVERED,READ para os leads que foram enviados, entregues e lidos. Sem status, retorna todos. O arquivo inclui uma coluna campaign identificando a origem de cada linha e BOM UTF-8 para abertura correta no Excel.
// @Tags			Campanhas do WhatsApp
// @Produce		json
// @Param			status					query	string	false	"Status dos envios (lista: SENT,DELIVERED,READ). Vazio = todos"
// @Param			from					query	string	false	"Data inicial de criação da campanha (YYYY-MM-DD ou RFC3339)"
// @Param			to						query	string	false	"Data final de criação da campanha (YYYY-MM-DD ou RFC3339)"
// @Param			type					query	string	false	"Tipo de campanha (standard/organic). Vazio = todos"
// @Param			stageId					query	string	false	"Filtrar por etapa"
// @Param			search					query	string	false	"Buscar por número"
// @Param			interest				query	string	false	"Filtrar por interesse"
// @Param			disposition				query	string	false	"Filtrar por disposição"
// @Param			sentiment				query	string	false	"Filtrar por sentimento"
// @Param			qualification			query	string	false	"Filtrar por qualificação"
// @Param			nextAction				query	string	false	"Filtrar por próxima ação"
// @Param			hasAnalysis				query	bool	false	"Filtrar por presença de análise"
// @Param			attendanceQualityMin	query	int		false	"Qualidade de atendimento mínima"
// @Param			attendanceQualityMax	query	int		false	"Qualidade de atendimento máxima"
// @Success		202	{object}	report.Job	"Relatorio na fila; acompanhe por /reports/{id}"
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		413	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/campaigns/entries/export [get]
func (h *ExportHandler) ExportWhatsAppWorkspaceEntries(w http.ResponseWriter, r *http.Request) {
	if middleware.GetWorkspaceID(r) == "" {
		response.WriteError(w, http.StatusForbidden, "workspace is required", nil)
		return
	}
	if httpx.ShouldReturnEmptyDepartmentList(r) {
		response.WriteError(w, http.StatusNotFound, "No entries to export", nil)
		return
	}

	filter, errs := h.parseExportFilter(r, "", exportdomain.EntryTypeWhatsApp)
	if errs != nil {
		response.WriteValidationError(w, errs)
		return
	}
	h.queueExport(w, r, filter, "whatsapp-leads")
}

func (h *ExportHandler) ExportInstagramEntries(w http.ResponseWriter, r *http.Request) {
	h.exportChannelEntries(w, r, exportdomain.EntryTypeInstagram, "instagram-account")
}

func (h *ExportHandler) ExportTelegramEntries(w http.ResponseWriter, r *http.Request) {
	h.exportChannelEntries(w, r, exportdomain.EntryTypeTelegram, "telegram-account")
}

func (h *ExportHandler) ExportUnofficialWhatsAppCampaignEntries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	q.Set("type", "campaign")
	r.URL.RawQuery = q.Encode()

	h.exportChannelEntries(w, r,
		exportdomain.EntryTypeUnofficialWhatsApp, "unofficial-whatsapp-campaign")
}

func (h *ExportHandler) exportChannelEntries(
	w http.ResponseWriter,
	r *http.Request,
	entryType exportdomain.EntryType,
	filenamePrefix string,
) {
	accountID := mux.Vars(r)["id"]
	if middleware.GetWorkspaceID(r) == "" {
		response.WriteError(w, http.StatusForbidden, "workspace is required", nil)
		return
	}
	filter, errs := h.parseExportFilter(r, accountID, entryType)
	if errs != nil {
		response.WriteValidationError(w, errs)
		return
	}
	h.queueExport(w, r, filter, fmt.Sprintf("%s-%s", filenamePrefix, sanitizeFilenamePart(accountID)))
}

func (h *ExportHandler) parseExportFilter(
	r *http.Request,
	containerID string,
	entryType exportdomain.EntryType,
) (exportdomain.ExportFilter, map[string]string) {
	values := r.URL.Query()

	statuses, err := parseStatuses(values, entryType)
	if err != nil {
		return exportdomain.ExportFilter{}, map[string]string{"status": err.Error()}
	}

	stageID := strings.TrimSpace(values.Get("stageId"))
	if stageID == "" {
		stageID = strings.TrimSpace(values.Get("StageID"))
	}

	filter := exportdomain.ExportFilter{
		Scope: exportdomain.Scope{
			WorkspaceID:   middleware.GetWorkspaceID(r),
			ContainerID:   containerID,
			ContainerType: strings.TrimSpace(values.Get("type")),
			DepartmentIDs: httpx.DepartmentFilterIDs(r),
			Statuses:      statuses,
			CreatedFrom:   httpx.ParseDateBound(values.Get("from"), false),
			CreatedTo:     httpx.ParseDateBound(values.Get("to"), true),
		},
		EntryType:     entryType,
		StageID:       stageID,
		Number:        strings.TrimSpace(values.Get("search")),
		Interest:      strings.TrimSpace(values.Get("interest")),
		Disposition:   strings.TrimSpace(values.Get("disposition")),
		Sentiment:     strings.TrimSpace(values.Get("sentiment")),
		Qualification: strings.TrimSpace(values.Get("qualification")),
		NextAction:    strings.TrimSpace(values.Get("nextAction")),
	}

	if hasAnalysis := values.Get("hasAnalysis"); hasAnalysis != "" {
		val := strings.ToLower(hasAnalysis) == "true"
		filter.HasAnalysis = &val
	}
	if minStr := values.Get("attendanceQualityMin"); minStr != "" {
		if min, err := strconv.Atoi(minStr); err == nil {
			filter.AttendanceQualityMin = &min
		}
	}
	if maxStr := values.Get("attendanceQualityMax"); maxStr != "" {
		if max, err := strconv.Atoi(maxStr); err == nil {
			filter.AttendanceQualityMax = &max
		}
	}

	return filter, nil
}

func parseStatuses(values url.Values, entryType exportdomain.EntryType) ([]string, error) {
	raw := values["status"]
	if len(raw) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		for _, part := range strings.Split(item, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			switch entryType {
			case exportdomain.EntryTypeWhatsApp:
				part = strings.ToUpper(part)
				if !wce.ValidStatus(wce.SendStatus(part)) {
					return nil, fmt.Errorf("unknown status %q", part)
				}
			case exportdomain.EntryTypeUnofficialWhatsApp:
				part = strings.ToUpper(part)
				if !uwc.ValidStatus(campaign.SendStatus(part)) {
					return nil, fmt.Errorf("unknown status %q", part)
				}
			}
			if _, dup := seen[part]; dup {
				continue
			}
			seen[part] = struct{}{}
			out = append(out, part)
		}
	}
	return out, nil
}

func (h *ExportHandler) queueExport(
	w http.ResponseWriter,
	r *http.Request,
	filter exportdomain.ExportFilter,
	label string,
) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	params, err := json.Marshal(report_renderers.ConversationEntriesParams{
		Filter: filter,
		Label:  label,
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to prepare the export", nil)
		return
	}

	job, err := h.reports.Create(report_usecase.CreateInput{
		WorkspaceID: filter.Scope.WorkspaceID,
		RequestedBy: claims.UserID,
		Kind:        reportdomain.KindConversationEntries,
		Format:      reportdomain.FormatCSV,
		Locale:      httpx.RequestLocale(r),
		Params:      params,
	})
	if err != nil {
		h.writeExportError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusAccepted, job)
}

func (h *ExportHandler) writeExportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, report_usecase.ErrNotConfigured):
		response.WriteError(w, http.StatusServiceUnavailable,
			"Exports are not configured on this server.", nil)
	case errors.Is(err, exportdomain.ErrTooManyRows):
		response.WriteError(w, http.StatusRequestEntityTooLarge,
			"This export is too large. Narrow the period or the filters and try again.", nil)
	case errors.Is(err, reportdomain.ErrParamsTooLarge):
		response.WriteError(w, http.StatusBadRequest,
			"Too many filters for one export. Narrow them and try again.", nil)
	default:
		log.Printf("[export] queueing failed: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "Failed to queue the export", nil)
	}
}

func sanitizeFilenamePart(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			return r
		default:
			return -1
		}
	}, s)
}
