package export

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/campaign"
	exportdomain "vozko/domain/export"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	whatsappcampaign_usecase "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/infra/http/middleware"
)

const (
	maxConcurrentExports = 3

	exportQueueWait = 15 * time.Second

	exportTimeout = 5 * time.Minute
)

type ExportHandler struct {
	exportUC exportdomain.ExportEntriesUseCase
	getWCUC  whatsappcampaign_usecase.GetCampaignUseCase

	slots chan struct{}
}

func NewExportHandler(
	exportUC exportdomain.ExportEntriesUseCase,
	getWCUC whatsappcampaign_usecase.GetCampaignUseCase,
) *ExportHandler {
	return &ExportHandler{
		exportUC: exportUC,
		getWCUC:  getWCUC,
		slots:    make(chan struct{}, maxConcurrentExports),
	}
}

// @Summary		Exportar entradas de campanha do WhatsApp (CSV)
// @Description	Exporta em CSV as entradas (contatos) de uma campanha do WhatsApp do workspace, aplicando os filtros informados na query. O parâmetro status aceita múltiplos valores, separados por vírgula ou repetidos. O arquivo inclui BOM UTF-8 para abertura correta no Excel.
// @Tags			Campanhas do WhatsApp
// @Produce		text/csv
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
// @Success		200	{file}		binary	"Arquivo CSV das entradas"
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		429	{object}	response.ErrorResponse
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
	h.writeCSVExport(w, r, filter, fmt.Sprintf("whatsapp-campaign-%s", req.CampaignID))
}

// @Summary		Exportar leads dos disparos do WhatsApp (CSV)
// @Description	Exporta em CSV os leads de TODAS as campanhas do workspace de uma só vez, no mesmo recorte do resumo de disparos (período de criação da campanha, tipo e departamento). Use status para escolher os envios desejados — por exemplo status=SENT,DELIVERED,READ para os leads que foram enviados, entregues e lidos. Sem status, retorna todos. O arquivo inclui uma coluna campaign identificando a origem de cada linha e BOM UTF-8 para abertura correta no Excel.
// @Tags			Campanhas do WhatsApp
// @Produce		text/csv
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
// @Success		200	{file}		binary	"Arquivo CSV dos leads"
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		413	{object}	response.ErrorResponse
// @Failure		429	{object}	response.ErrorResponse
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
	h.writeCSVExport(w, r, filter, "whatsapp-leads")
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
	h.writeCSVExport(w, r, filter, fmt.Sprintf("%s-%s", filenamePrefix, sanitizeFilenamePart(accountID)))
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

func (h *ExportHandler) writeCSVExport(
	w http.ResponseWriter,
	r *http.Request,
	filter exportdomain.ExportFilter,
	filenamePrefix string,
) {
	release, ok := h.acquireSlot(r.Context())
	if !ok {
		w.Header().Set("Retry-After", "30")
		response.WriteError(w, http.StatusTooManyRequests,
			"Too many exports running right now. Please try again in a moment.", nil)
		return
	}
	defer release()

	ctx, cancel := context.WithTimeout(r.Context(), exportTimeout)
	defer cancel()

	filename := fmt.Sprintf("%s-%s.csv", filenamePrefix, time.Now().Format("2006-01-02"))
	sink := &csvResponse{w: w, filename: filename}

	count, err := h.exportUC.Export(ctx, filter, sink)
	if err != nil {
		if sink.started {
			log.Printf("[export] aborting partial CSV after %d rows: %v", count, err)
			panic(http.ErrAbortHandler)
		}
		h.writeExportError(w, err)
		return
	}

	if count == 0 {
		response.WriteError(w, http.StatusNotFound, "No entries to export", nil)
		return
	}
}

func (h *ExportHandler) writeExportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, exportdomain.ErrTooManyRows):
		response.WriteError(w, http.StatusRequestEntityTooLarge,
			"This export is too large. Narrow the period or the filters and try again.", nil)
	case errors.Is(err, context.DeadlineExceeded):
		response.WriteError(w, http.StatusGatewayTimeout,
			"The export took too long. Narrow the period and try again.", nil)
	case errors.Is(err, context.Canceled):
		return
	default:
		log.Printf("[export] failed: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "Failed to export entries", nil)
	}
}

func (h *ExportHandler) acquireSlot(ctx context.Context) (func(), bool) {
	timer := time.NewTimer(exportQueueWait)
	defer timer.Stop()

	select {
	case h.slots <- struct{}{}:
		return func() { <-h.slots }, true
	case <-ctx.Done():
		return nil, false
	case <-timer.C:
		return nil, false
	}
}

type csvResponse struct {
	w        http.ResponseWriter
	filename string
	started  bool
}

func (c *csvResponse) Write(p []byte) (int, error) {
	if !c.started {
		c.started = true
		c.w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		c.w.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename=%q", sanitizeFilenamePart(c.filename)))
		c.w.Header().Set("X-Content-Type-Options", "nosniff")
		c.w.Header().Set("Cache-Control", "no-store")
		c.w.WriteHeader(http.StatusOK)
		if _, err := c.w.Write([]byte("\xEF\xBB\xBF")); err != nil {
			return 0, err
		}
	}

	n, err := c.w.Write(p)
	if flusher, ok := c.w.(http.Flusher); ok {
		flusher.Flush()
	}
	return n, err
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
