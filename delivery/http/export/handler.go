package export

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	exportdomain "vozko/domain/export"
	whatsappcampaign_usecase "vozko/domain/whatsapp_campaign"
	"vozko/infra/http/middleware"
)

type ExportHandler struct {
	exportUC exportdomain.ExportEntriesUseCase
	getWCUC  whatsappcampaign_usecase.GetCampaignUseCase
}

func NewExportHandler(
	exportUC exportdomain.ExportEntriesUseCase,
	getWCUC whatsappcampaign_usecase.GetCampaignUseCase,
) *ExportHandler {
	return &ExportHandler{
		exportUC: exportUC,
		getWCUC:  getWCUC,
	}
}

// @Summary		Exportar entradas de campanha do WhatsApp (CSV)
// @Description	Exporta em CSV as entradas (contatos) de uma campanha do WhatsApp do workspace, aplicando os filtros informados na query. O arquivo inclui BOM UTF-8 para abertura correta no Excel.
// @Tags			Campanhas do WhatsApp
// @Produce		text/csv
// @Param			id						path	string	true	"Identificador da campanha"
// @Param			status					query	string	false	"Filtrar por status da entrada"
// @Param			StageID					query	string	false	"Filtrar por etapa"
// @Param			search					query	string	false	"Buscar por número"
// @Param			interest				query	string	false	"Filtrar por interesse"
// @Param			disposition				query	string	false	"Filtrar por disposição"
// @Param			sentiment				query	string	false	"Filtrar por sentimento"
// @Param			qualification			query	string	false	"Filtrar por qualificação"
// @Param			nextAction				query	string	false	"Filtrar por próxima ação"
// @Param			hasAnalysis				query	bool	false	"Filtrar por presença de análise"
// @Param			attendanceQualityMin	query	int		false	"Qualidade de atendimento mínima"
// @Param			attendanceQualityMax	query	int		false	"Qualidade de atendimento máxima"
// @Success		200	{file}		binary	"Arquivo CSV das entradas"
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

	filter := h.parseExportFilter(r, req.CampaignID, exportdomain.EntryTypeWhatsApp)
	h.writeCSVExport(w, filter, fmt.Sprintf("whatsapp-campaign-%s", req.CampaignID))
}

func (h *ExportHandler) parseExportFilter(r *http.Request, campaignID string, entryType exportdomain.EntryType) exportdomain.ExportFilter {
	values := r.URL.Query()
	filter := exportdomain.ExportFilter{
		CampaignID:    campaignID,
		WorkspaceID:   middleware.GetWorkspaceID(r),
		EntryType:     entryType,
		Status:        strings.TrimSpace(values.Get("status")),
		StageID:       strings.TrimSpace(values.Get("StageID")),
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

	return filter
}

func (h *ExportHandler) writeCSVExport(w http.ResponseWriter, filter exportdomain.ExportFilter, filenamePrefix string) {
	var buf bytes.Buffer

	buf.Write([]byte("\xEF\xBB\xBF"))

	count, err := h.exportUC.Export(filter, &buf)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to export entries", nil)
		return
	}

	if count == 0 {
		response.WriteError(w, http.StatusNotFound, "No entries to export", nil)
		return
	}

	filename := fmt.Sprintf("%s-%s.csv", filenamePrefix, time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Write(buf.Bytes())
}
