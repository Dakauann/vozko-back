package opportunity

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"vozko/delivery/http/response"
	"vozko/infra/http/middleware"
	"vozko/usecases/opportunityio"
)

const maxImportBytes = 10 << 20

// @Summary		Exportar oportunidades (CSV)
// @Description	Exporta as oportunidades de um pipeline do workspace em formato CSV, respeitando o escopo de departamento do usuário. As colunas incluem id, título, valor (em unidades maiores), moeda, status, etapa, responsável, lead, origem, data de fechamento, data de criação e uma coluna por campo personalizado.
// @Tags			Oportunidades
// @Produce		text/csv
// @Param			pipelineId	query	string	true	"ID do pipeline"
// @Success		200	{file}		file	"Arquivo CSV das oportunidades"
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

	deptIDs, restrict, override, allowed := h.resolveScope(claims.UserID, wsID, claims.Role == "admin")
	if !allowed {
		response.WriteError(w, http.StatusForbidden, "Forbidden", nil)
		return
	}

	var buf bytes.Buffer
	buf.Write([]byte("\xEF\xBB\xBF"))
	if _, err := h.io.Export(wsID, pipelineID, deptIDs, restrict, override, &buf); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to export opportunities", nil)
		return
	}

	filename := fmt.Sprintf("opportunities-%s-%s.csv", pipelineID, time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Write(buf.Bytes())
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
	if middleware.GetClaims(r) == nil {
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
