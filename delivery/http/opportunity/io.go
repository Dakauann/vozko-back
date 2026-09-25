package opportunity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	reportdomain "vozko/domain/report"
	"vozko/infra/http/middleware"
	"vozko/usecases/opportunityio"
	report_usecase "vozko/usecases/report"
	report_renderers "vozko/usecases/report/renderers"
)

const maxImportBytes = 10 << 20

// @Summary		Exportar oportunidades (CSV)
// @Description	Exporta as oportunidades de um pipeline do workspace em formato CSV, respeitando o escopo de departamento do usuário. As colunas incluem id, título, valor (em unidades maiores), moeda, status, etapa, responsável, lead, origem, data de fechamento, data de criação e uma coluna por campo personalizado.
// @Tags			Oportunidades
// @Produce		json
// @Param			pipelineId	query	string	true	"ID do pipeline"
// @Param			format		query	string	false	"Formato (csv ou pdf)"
// @Success		202	{object}	report.Job	"Relatorio na fila"
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/opportunities/export [get]
func (h *OpportunityHandler) Export(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)
	pipelineID := strings.TrimSpace(r.URL.Query().Get("pipelineId"))
	if pipelineID == "" {
		response.WriteError(w, http.StatusBadRequest, "pipelineId is required", nil)
		return
	}

	scope, err := h.deals.Scope(personFrom(claims), wsID)
	if err != nil {
		response.WriteError(w, http.StatusForbidden, "Forbidden", nil)
		return
	}

	if h.reports == nil {
		response.WriteError(w, http.StatusServiceUnavailable,
			"Exports are not configured on this server", nil)
		return
	}

	format := reportdomain.Format(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format"))))
	if format == "" {
		format = reportdomain.FormatCSV
	}

	params, err := json.Marshal(report_renderers.OpportunitiesParams{
		PipelineID:             pipelineID,
		DepartmentIDs:          scope.DepartmentIDs,
		Restrict:               scope.Restrict,
		AssigneeOverrideUserID: scope.AssigneeOverride,
		Label:                  "oportunidades",
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to prepare the export", nil)
		return
	}

	job, err := h.reports.Create(report_usecase.CreateInput{
		WorkspaceID: wsID,
		RequestedBy: claims.UserID,
		Kind:        reportdomain.KindOpportunities,
		Format:      format,
		Locale:      httpx.RequestLocale(r),
		Params:      params,
	})
	if err != nil {
		switch {
		case errors.Is(err, report_usecase.ErrNotConfigured):
			response.WriteError(w, http.StatusServiceUnavailable,
				"Exports are not configured on this server", nil)
		case errors.Is(err, reportdomain.ErrInvalidFormat),
			errors.Is(err, reportdomain.ErrFormatUnsupported):
			response.WriteValidationError(w, map[string]string{"format": "must be csv or pdf"})
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to queue the export", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusAccepted, job)
}

// @Summary		Importar oportunidades (CSV)
// @Description	Cria uma oportunidade por linha válida a partir de um CSV enviado como upload multipart (campo 'file') ou como corpo bruto. Com dryRun=true nada é criado e cada linha é apenas validada. O parâmetro pipelineId é o pipeline padrão para linhas que omitem a coluna pipeline_id. A resposta é um relatório com total, criados, ignorados, erros por linha e indicador de truncamento.
// @Tags			Oportunidades
// @Accept			multipart/form-data
// @Produce		json
// @Param			dryRun		query		bool	false	"Quando true, apenas valida sem criar"
// @Param			pipelineId	query		string	false	"Pipeline padrão para linhas sem pipeline_id"
// @Param			file		formData	file	true	"Arquivo CSV a importar"
// @Success		200	{object}	opportunityio.ImportReport
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/opportunities/import [post]
func (h *OpportunityHandler) Import(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	body, err := readCSVBody(r)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	dryRun := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("dryRun")), "true")

	report, err := h.io.Import(wsID, bytes.NewReader(body), opportunityio.ImportOptions{
		DefaultPipelineID: strings.TrimSpace(r.URL.Query().Get("pipelineId")),
		DryRun:            dryRun,
		ActorID:           claims.UserID,
	})
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "Failed to parse CSV", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, report)
}

func readCSVBody(r *http.Request) ([]byte, error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(maxImportBytes); err != nil {
			return nil, fmt.Errorf("invalid multipart form")
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			return nil, fmt.Errorf("missing 'file' upload field")
		}
		defer file.Close()
		return io.ReadAll(io.LimitReader(file, maxImportBytes))
	}

	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, maxImportBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read request body")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty CSV body")
	}
	return data, nil
}
