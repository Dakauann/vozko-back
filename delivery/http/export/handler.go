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
	exportdomain "vozko/domain/export"
	whatsappcampaign_usecase "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/infra/http/middleware"
)

const (
	// maxConcurrentExports bounds how many exports this instance runs at once.
	//
	// An export is the heaviest read in the product: it walks a workspace's
	// entire campaign history twice and holds a database connection for each
	// page. The pool is 10 wide and shared with every interactive request on the
	// instance, so without a ceiling one tenant pulling a year of disparos makes
	// the inbox slow for everyone else. Three leaves the pool room to serve the
	// people who are not exporting.
	maxConcurrentExports = 3

	// exportQueueWait is how long a request waits for a slot before giving up.
	// Long enough that a double-click or two colleagues exporting at once just
	// queue; short enough that nobody stares at a dead tab.
	exportQueueWait = 15 * time.Second

	// exportTimeout is the ceiling on one export. It bounds how long a slot and
	// its connections can be held by a query that has stopped making progress.
	exportTimeout = 5 * time.Minute
)

type ExportHandler struct {
	exportUC exportdomain.ExportEntriesUseCase
	getWCUC  whatsappcampaign_usecase.GetCampaignUseCase

	// slots is the concurrency guard. Buffered to maxConcurrentExports; a send
	// acquires, a receive releases.
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
	// A campaign the caller's department scope excludes is not theirs to export,
	// even though it is their workspace's. Same rule the campaign list applies,
	// from the same function — an export must not reach what the list hides.
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
	// Department-scoped to nothing means visible-to-nothing, which is not the
	// same as an unscoped caller seeing everything. Same rule the summary uses.
	if httpx.ShouldReturnEmptyDepartmentList(r) {
		response.WriteError(w, http.StatusNotFound, "No entries to export", nil)
		return
	}

	// No container id: every campaign the scope allows. Tenancy and department
	// scope are carried on the Scope itself and enforced in SQL.
	filter, errs := h.parseExportFilter(r, "", exportdomain.EntryTypeWhatsApp)
	if errs != nil {
		response.WriteValidationError(w, errs)
		return
	}
	h.writeCSVExport(w, r, filter, "whatsapp-leads")
}

// ExportInstagramEntries exports one Instagram account's conversations.
//
// The account id fills the container slot: it is the channel's container, which
// is the same question a campaign filter asks on WhatsApp.
func (h *ExportHandler) ExportInstagramEntries(w http.ResponseWriter, r *http.Request) {
	h.exportChannelEntries(w, r, exportdomain.EntryTypeInstagram, "instagram-account")
}

// ExportTelegramEntries exports one Telegram bot's conversations.
func (h *ExportHandler) ExportTelegramEntries(w http.ResponseWriter, r *http.Request) {
	h.exportChannelEntries(w, r, exportdomain.EntryTypeTelegram, "telegram-account")
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
	// The account's own tenancy is enforced by the lister's workspace filter, so
	// a caller cannot export another workspace's account by guessing its id.
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

	// Both spellings of the stage filter. It was declared as StageID while every
	// caller in the product sends stageId — the camelCase spelling every other
	// filter here uses — so the stage filter has never once applied to an
	// export. Reading both fixes it without breaking a client that learned the
	// old name from the Swagger docs.
	stageID := strings.TrimSpace(values.Get("stageId"))
	if stageID == "" {
		stageID = strings.TrimSpace(values.Get("StageID"))
	}

	filter := exportdomain.ExportFilter{
		Scope: exportdomain.Scope{
			WorkspaceID:   middleware.GetWorkspaceID(r),
			ContainerID:   containerID,
			ContainerType: strings.TrimSpace(values.Get("type")),
			// The caller's own department scope, never a value they send. An
			// export must not be a way to read past it.
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

// parseStatuses reads the status filter, which accepts both a repeated
// parameter and a comma-separated list so callers can spell it either way.
//
// WhatsApp values are validated against the domain's closed set and an unknown
// one is rejected rather than ignored: silently dropping a misspelt status
// would widen the export to everything, and the operator would have no way to
// tell that the file they got is not the file they asked for. Other channels
// carry free-form conversation statuses, so there is no set to check against.
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
			if entryType == exportdomain.EntryTypeWhatsApp {
				part = strings.ToUpper(part)
				if !wce.SendStatus(part).Valid() {
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
		// Once bytes are on the wire the status code is already 200 and there is
		// no honest way to say "this failed". Abort the connection so the client
		// sees a broken transfer instead of a file that looks complete and is
		// silently short.
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
		// The caller went away; there is nobody left to answer.
		return
	default:
		log.Printf("[export] failed: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "Failed to export entries", nil)
	}
}

// acquireSlot takes one of the export slots, waiting briefly rather than
// refusing a caller who merely arrived at the same moment as someone else.
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

// csvResponse defers the HTTP response headers until the export produces its
// first byte.
//
// That is what lets writeCSVExport answer 404 for an empty result and 413 for
// an oversized one: both are decided inside Export, after a handler that
// committed its headers up front would already have promised a 200 and a file.
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
		// The response is attacker-influenced text; keep browsers from guessing
		// a renderable type for it.
		c.w.Header().Set("X-Content-Type-Options", "nosniff")
		c.w.Header().Set("Cache-Control", "no-store")
		c.w.WriteHeader(http.StatusOK)
		// BOM, so Excel opens accented Portuguese correctly instead of mojibake.
		if _, err := c.w.Write([]byte("\xEF\xBB\xBF")); err != nil {
			return 0, err
		}
	}

	n, err := c.w.Write(p)
	// Push each buffered chunk to the client as it is produced, so a large
	// export downloads progressively instead of appearing to hang.
	if flusher, ok := c.w.(http.Flusher); ok {
		flusher.Flush()
	}
	return n, err
}

// sanitizeFilenamePart strips anything that could break out of the
// Content-Disposition header. Part of the name comes from a URL path segment,
// so quotes, CR and LF would otherwise be caller-controlled header bytes.
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
