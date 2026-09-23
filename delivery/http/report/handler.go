package report

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	reportdomain "vozko/domain/report"
	"vozko/infra/http/middleware"
	report_usecase "vozko/usecases/report"
)

type ReportHandler struct {
	service *report_usecase.Service
}

func NewReportHandler(service *report_usecase.Service) *ReportHandler {
	return &ReportHandler{service: service}
}

type CreateReportRequest struct {
	Kind   string          `json:"kind" example:"attendance_overview"`
	Format string          `json:"format" example:"csv"`
	Locale string          `json:"locale,omitempty" example:"pt"`
	Params json.RawMessage `json:"params,omitempty" swaggertype:"object"`
}

func writeReportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, report_usecase.ErrNotConfigured):
		response.WriteError(w, http.StatusServiceUnavailable, "Reports are not configured on this server", nil)
	case errors.Is(err, reportdomain.ErrNotFound):
		response.WriteError(w, http.StatusNotFound, "Report not found", nil)
	case errors.Is(err, reportdomain.ErrForbidden):
		response.WriteError(w, http.StatusForbidden, "This report belongs to another workspace", nil)
	case errors.Is(err, reportdomain.ErrNotReady):
		response.WriteError(w, http.StatusConflict, "The report file is not ready yet", nil)
	case errors.Is(err, reportdomain.ErrExpired):
		response.WriteError(w, http.StatusGone, "The report file has expired", nil)
	case errors.Is(err, reportdomain.ErrUnknownKind),
		errors.Is(err, reportdomain.ErrInvalidFormat),
		errors.Is(err, reportdomain.ErrFormatUnsupported),
		errors.Is(err, reportdomain.ErrKindRequired),
		errors.Is(err, reportdomain.ErrParamsTooLarge):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Failed to handle the report: "+err.Error(), nil)
	}
}

// @Summary		Solicitar um relatório
// @Description	Coloca a geração do relatório na fila e devolve o identificador do trabalho. O arquivo fica pronto de forma assíncrona; acompanhe pelo status.
// @Tags			Relatórios
// @Accept			json
// @Produce		json
// @Param			request	body		CreateReportRequest	true	"Relatório a gerar"
// @Success		202	{object}	report.Job
// @Failure		400	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/reports [post]
func (h *ReportHandler) Create(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id required", nil)
		return
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var req CreateReportRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, reportdomain.MaxParamsBytes*2)).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"kind":   "string (required)",
			"format": "string: csv | xlsx | pdf",
			"params": "object (optional)",
		})
		return
	}

	job, err := h.service.Create(report_usecase.CreateInput{
		WorkspaceID: workspaceID,
		RequestedBy: claims.UserID,
		Kind:        reportdomain.Kind(strings.TrimSpace(req.Kind)),
		Format:      reportdomain.Format(strings.TrimSpace(req.Format)),
		Locale:      req.Locale,
		Params:      req.Params,
	})
	if err != nil {
		writeReportError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusAccepted, job)
}

// @Summary		Situação de um relatório
// @Description	Devolve a situação do relatório solicitado: na fila, processando, pronto, com falha ou expirado.
// @Tags			Relatórios
// @Produce		json
// @Param			id	path	string	true	"ID do relatório"
// @Success		200	{object}	report.Job
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/reports/{id} [get]
func (h *ReportHandler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id required", nil)
		return
	}
	job, err := h.service.Get(workspaceID, mux.Vars(r)["id"])
	if err != nil {
		writeReportError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, job)
}

// @Summary		Listar relatórios recentes
// @Description	Lista os relatórios solicitados recentemente pelo workspace, do mais novo para o mais antigo.
// @Tags			Relatórios
// @Produce		json
// @Param			limit	query	int		false	"Quantidade máxima (padrão 25, teto 100)"
// @Param			offset	query	int		false	"Deslocamento para paginação"
// @Param			kind	query	string	false	"Filtrar por tipo (aceita lista separada por vírgula)"
// @Param			status	query	string	false	"Filtrar por situação (aceita lista separada por vírgula)"
// @Param			from	query	string	false	"Data inicial (YYYY-MM-DD ou RFC3339)"
// @Param			to		query	string	false	"Data final (YYYY-MM-DD ou RFC3339)"
// @Success		200	{object}	report.ListPage
// @Security		BearerAuth
// @Router			/reports [get]
func (h *ReportHandler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id required", nil)
		return
	}

	values := r.URL.Query()
	query := reportdomain.ListQuery{
		WorkspaceID: workspaceID,
		RequestedBy: strings.TrimSpace(values.Get("requestedBy")),
		Limit:       intParam(values.Get("limit"), reportdomain.DefaultListLimit),
		Offset:      intParam(values.Get("offset"), 0),
	}

	for _, raw := range splitList(values["kind"]) {
		query.Kinds = append(query.Kinds, reportdomain.Kind(raw))
	}
	for _, raw := range splitList(values["status"]) {
		query.Statuses = append(query.Statuses, reportdomain.Status(raw))
	}
	query.CreatedFrom = httpx.ParseDateBound(values.Get("from"), false)
	query.CreatedTo = httpx.ParseDateBound(values.Get("to"), true)

	page, err := h.service.List(query)
	if err != nil {
		writeReportError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, page)
}

func intParam(raw string, fallback int) int {
	if parsed, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
		return parsed
	}
	return fallback
}

func splitList(values []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.ToLower(strings.TrimSpace(part))
			if part == "" {
				continue
			}
			if _, duplicate := seen[part]; duplicate {
				continue
			}
			seen[part] = struct{}{}
			out = append(out, part)
		}
	}
	return out
}

// @Summary		Baixar o arquivo do relatório
// @Description	Devolve o arquivo gerado. Responde 409 enquanto o relatório não está pronto e 410 quando já expirou.
// @Tags			Relatórios
// @Produce		json
// @Param			id	path	string	true	"ID do relatório"
// @Success		200	{string}	string	"Arquivo do relatório"
// @Failure		409	{object}	response.ErrorResponse
// @Failure		410	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/reports/{id}/file [get]
func (h *ReportHandler) Download(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id required", nil)
		return
	}
	file, err := h.service.File(r.Context(), workspaceID, mux.Vars(r)["id"])
	if err != nil {
		writeReportError(w, err)
		return
	}

	filename := file.Filename
	if filename == "" {
		filename = "report"
	}
	w.Header().Set("Content-Type", file.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(file.Data)))
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Header().Set("X-Report-Filename", filename)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(file.Data)
}

// @Summary		Tipos de relatório disponíveis
// @Description	Lista os tipos de relatório que este servidor sabe gerar e os formatos aceitos por cada um.
// @Tags			Relatórios
// @Produce		json
// @Success		200	{object}	map[string][]report.KindDescriptor
// @Security		BearerAuth
// @Router			/reports/kinds [get]
func (h *ReportHandler) Kinds(w http.ResponseWriter, r *http.Request) {
	if h.service == nil || h.service.Registry() == nil {
		response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
			"kinds": []reportdomain.KindDescriptor{},
		})
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"kinds": h.service.Registry().Descriptors(),
	})
}

// @Summary		Dados para a página de impressão
// @Description	Devolve os dados do relatório para a página que o navegador headless imprime em PDF. Exige um token de impressão de curta duração, válido apenas para um relatório.
// @Tags			Relatórios
// @Produce		json
// @Param			id		path	string	true	"ID do relatório"
// @Param			token	query	string	true	"Token de impressão"
// @Success		200	{object}	report_usecase.PrintPayload
// @Failure		401	{object}	response.ErrorResponse
// @Router			/reports/{id}/print-data [get]
func (h *ReportHandler) PrintData(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		response.WriteError(w, http.StatusUnauthorized, "A print token is required", nil)
		return
	}

	payload, err := h.service.PrintPayload(r.Context(), token)
	if err != nil {
		switch {
		case errors.Is(err, reportdomain.ErrPrintTokenExpired):
			response.WriteError(w, http.StatusUnauthorized, "The print token has expired", nil)
		case errors.Is(err, reportdomain.ErrPrintTokenInvalid),
			errors.Is(err, reportdomain.ErrPrintSecretUnset):
			response.WriteError(w, http.StatusUnauthorized, "The print token is not valid", nil)
		default:
			writeReportError(w, err)
		}
		return
	}

	if payload.Job.ID != mux.Vars(r)["id"] {
		response.WriteError(w, http.StatusUnauthorized, "The print token is not valid for this report", nil)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	response.WriteSuccess(w, http.StatusOK, payload)
}
