package workflowrunexporthttp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/report"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	workflow_usecase "vozko/usecases/workflow"
)

type Exports interface {
	Run(viewer shared.Person, scope workflow.Scope, runID string) (*workflow.RunExport, error)
	Stream(ctx context.Context, viewer shared.Person, scope workflow.Scope, workflowID string, filter workflow.RunExportFilter, sink workflow_usecase.RunExportSink) error
}

type exportTooLargeDetails struct {
	MaxRuns int `json:"maxRuns" example:"5000"`
	MaxDays int `json:"maxDays" example:"31"`
}

var (
	refusalRunExportFailed = response.Refusal{Status: http.StatusInternalServerError, Code: "run_export_failed", Message: "Não foi possível exportar as execuções. Tente de novo."}
	refusalInvalidPeriod   = response.Refusal{Status: http.StatusBadRequest, Code: "export_period_invalid", Message: "Escolha um período válido: a data inicial precisa vir antes da final, e o status, se informado, precisa existir."}.Expecting(map[string]string{
		"from":    "data e hora RFC 3339 (obrigatório), início do período",
		"to":      "data e hora RFC 3339 (obrigatório), fim do período, depois de from, até 31 dias",
		"status":  "running, waiting, completed, error ou cancelled (opcional)",
		"entryId": "ID da conversa (opcional)",
	})
	refusalExportTooLarge = response.Refusal{Status: http.StatusUnprocessableEntity, Code: "export_too_large", Message: "A exportação passa de 5.000 execuções ou de 31 dias. Escolha um período menor ou filtre por status ou conversa."}
)

var exportRefusals = append(append([]response.ErrorRefusal{}, httpx.WorkflowAccessRefusals...),
	response.ErrorRefusal{Err: workflow.ErrRunNotFound, Refusal: response.Refusal{Status: http.StatusNotFound, Code: "run_not_found", Message: "Execução do workflow não encontrada"}},
	response.ErrorRefusal{Err: workflow.ErrRunExportFilterInvalid, Refusal: refusalInvalidPeriod},
	response.ErrorRefusal{Err: workflow.ErrRunExportTooLarge, Refusal: refusalExportTooLarge},
)

type Handler struct {
	exports Exports
}

func NewHandler(exports Exports) *Handler {
	return &Handler{exports: exports}
}

// @Summary Baixar uma execução de workflow em JSON
// @Description Devolve a execução como um documento JSON para download (Content-Disposition attachment, nome com o fluxo e o ID da execução). O documento é a mesma leitura de GET /workflow-runs/{runId}, com todas as etapas: schemaVersion, exportedAt, run (ID, workflowId, workflowName, versão, status, gatilho, startedBy, início, fim, duração em ms, motivo da espera, etapa atual, erro e agentResult), triggerData, finalState (o estado final) e steps, cada etapa como o detalhe a devolve (parâmetros usados, saída, tipo e cadeia de erro, stack, chamada HTTP, logs do script, unresolvedRefs, tempos). Os mesmos valores mascarados do detalhe; segredos nunca aparecem. Exige workflows:read e só encontra execuções do workspace atual em conversas que quem pede pode ver (mesma regra de departamento e de responsável da caixa de entrada); fora disso a resposta é 404.
// @Tags workflows
// @Produce json
// @Param runId path string true "ID da execução"
// @Success 200 {object} workflow.RunExport
// @Failure 401 {object} response.ErrorResponse
// @Failure 403 {object} response.ErrorResponse "workflow_access_denied: workflow de outro departamento"
// @Failure 404 {object} response.ErrorResponse "run_not_found: execução não encontrada ou em conversa que você não pode ver"
// @Failure 500 {object} response.ErrorResponse "run_export_failed"
// @Security BearerAuth
// @Router /workflow-runs/{runId}/export [get]
func (h *Handler) ExportRun(w http.ResponseWriter, r *http.Request) {
	viewer, ok := httpx.RequirePerson(w, r)
	if !ok {
		return
	}
	doc, err := h.exports.Run(viewer, httpx.WorkflowScope(r), mux.Vars(r)["runId"])
	if err != nil {
		writeExportRefusal(w, err)
		return
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		writeExportRefusal(w, err)
		return
	}
	setAttachment(w, "application/json; charset=utf-8", report.Filename("json", "execucao", doc.Run.WorkflowName, doc.Run.ID))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// @Summary Exportar as execuções de um workflow em NDJSON
// @Description Baixa as execuções do workflow no período [from, to) como NDJSON: uma execução por linha, cada linha no mesmo formato de GET /workflow-runs/{runId}/export, da mais recente para a mais antiga. Usa os filtros da lista de execuções (status e entryId) e só inclui execuções em conversas que quem pede pode ver, filtradas na própria consulta. O arquivo é enviado aos poucos, em páginas. Limite por arquivo: 31 dias e 5.000 execuções; acima disso a resposta é 422 export_too_large, com details.maxRuns e details.maxDays: escolha um período menor ou filtre por status ou conversa. Se a leitura falhar depois do início do envio, a conexão é interrompida e o download falha, nunca termina como um arquivo completo.
// @Tags workflows
// @Produce application/x-ndjson
// @Param id path string true "ID do workflow"
// @Param from query string true "Início do período (RFC 3339, incluído)" example(2026-10-01T03:00:00Z)
// @Param to query string true "Fim do período (RFC 3339, excluído), até 31 dias depois de from" example(2026-10-08T03:00:00Z)
// @Param status query string false "Filtra pelo status" Enums(running, waiting, completed, error, cancelled)
// @Param entryId query string false "Filtra pela conversa"
// @Success 200 {object} workflow.RunExport "Uma linha por execução"
// @Failure 400 {object} response.ErrorResponse "export_period_invalid: período ausente ou inválido, ou status desconhecido"
// @Failure 401 {object} response.ErrorResponse
// @Failure 403 {object} response.ErrorResponse "workflow_access_denied: workflow de outro departamento"
// @Failure 404 {object} response.ErrorResponse "workflow_not_found"
// @Failure 422 {object} response.ErrorResponse{details=exportTooLargeDetails} "export_too_large: mais de 5.000 execuções ou mais de 31 dias"
// @Failure 500 {object} response.ErrorResponse "run_export_failed"
// @Security BearerAuth
// @Router /workflows/{id}/runs/export [get]
func (h *Handler) ExportRuns(w http.ResponseWriter, r *http.Request) {
	viewer, ok := httpx.RequirePerson(w, r)
	if !ok {
		return
	}
	filter, ok := exportFilterOf(r.URL.Query())
	if !ok {
		response.WriteRefusal(w, refusalInvalidPeriod)
		return
	}
	scope := httpx.WorkflowScope(r)
	sink := &ndjsonSink{w: w, filename: func(workflowName string) string {
		return report.Filename("ndjson", "execucoes", workflowName, filter.From.UTC().Format(time.DateOnly), filter.To.Add(-time.Second).UTC().Format(time.DateOnly))
	}}
	err := h.exports.Stream(r.Context(), viewer, scope, mux.Vars(r)["id"], filter, sink)
	switch {
	case err == nil:
		if !sink.started {
			_ = sink.Start("")
		}
	case !sink.started:
		writeExportRefusal(w, err)
	case r.Context().Err() != nil:
		slog.Info("workflow.runs_export_left", slog.String("workspace_id", scope.WorkspaceID), slog.String("error", err.Error()))
	default:
		slog.Error("workflow.runs_export_broken", slog.String("workspace_id", scope.WorkspaceID), slog.String("error", err.Error()))
		panic(http.ErrAbortHandler)
	}
}

func exportFilterOf(values url.Values) (workflow.RunExportFilter, bool) {
	from, fromErr := time.Parse(time.RFC3339, values.Get("from"))
	to, toErr := time.Parse(time.RFC3339, values.Get("to"))
	if fromErr != nil || toErr != nil {
		return workflow.RunExportFilter{}, false
	}
	filter := workflow.RunExportFilter{From: from, To: to, Runs: workflow.ListRunsInput{EntryID: values.Get("entryId")}}
	if s := values.Get("status"); s != "" {
		status := workflow.RunStatus(s)
		filter.Runs.Status = &status
	}
	return filter, true
}

func writeExportRefusal(w http.ResponseWriter, err error) {
	if errors.Is(err, workflow.ErrRunExportTooLarge) {
		response.WriteErrorWithDetails(w, refusalExportTooLarge.Status, refusalExportTooLarge.Code, refusalExportTooLarge.Message, exportTooLargeDetails{
			MaxRuns: workflow.MaxRunsPerExport,
			MaxDays: int(workflow.MaxRunExportPeriod / (24 * time.Hour)),
		})
		return
	}
	if response.WriteErrorRefusal(w, err, exportRefusals) {
		return
	}
	slog.Error("workflow.run_export_failed", slog.String("error", err.Error()))
	response.WriteRefusal(w, refusalRunExportFailed)
}

func setAttachment(w http.ResponseWriter, contentType, filename string) {
	header := w.Header()
	header.Set("Content-Type", contentType)
	header.Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Cache-Control", "no-store")
}

type ndjsonSink struct {
	w        http.ResponseWriter
	filename func(workflowName string) string
	encoder  *json.Encoder
	started  bool
}

func (s *ndjsonSink) Start(workflowName string) error {
	setAttachment(s.w, "application/x-ndjson; charset=utf-8", s.filename(workflowName))
	s.w.WriteHeader(http.StatusOK)
	s.encoder = json.NewEncoder(s.w)
	s.encoder.SetEscapeHTML(false)
	s.started = true
	return nil
}

func (s *ndjsonSink) Write(doc workflow.RunExport) error {
	return s.encoder.Encode(doc)
}

func (s *ndjsonSink) Flush() error {
	if err := http.NewResponseController(s.w).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}
