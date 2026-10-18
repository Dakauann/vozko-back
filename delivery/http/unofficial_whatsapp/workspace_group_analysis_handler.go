package unofficial_whatsapp

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	uwg "vozko/domain/unofficial_whatsapp_group"
	uwguc "vozko/usecases/unofficial_whatsapp_group"
)

// @Summary		Listar as análises dos grupos
// @Description	Análises de IA dos grupos do escopo, com o grupo, o último ciclo e o sentimento recente.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			status		query		string	false	"active, paused ou archived"
// @Param			trigger		query		string	false	"message_count ou scheduled"
// @Param			groupId		query		string	false	"Grupo"
// @Param			search		query		string	false	"Nome da análise ou do grupo"
// @Param			sort		query		string	false	"recent, oldest ou name"
// @Param			page		query		int		false	"Página"
// @Param			pageSize	query		int		false	"Itens por página"
// @Success		200			{object}	response.PaginatedPayload{data=[]unofficial_whatsapp_group.AnalysisRow}
// @Failure		403			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/analyses [get]
func (h *WorkspaceGroupHandler) ListAnalyses(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	values := r.URL.Query()
	page, err := h.groups.ListAnalyses(r.Context(), a, uwguc.AnalysisListInput{
		Status:     uwg.AnalysisStatus(strings.TrimSpace(values.Get("status"))),
		Trigger:    uwg.Trigger(strings.TrimSpace(values.Get("trigger"))),
		GroupID:    strings.TrimSpace(values.Get("groupId")),
		Search:     strings.TrimSpace(values.Get("search")),
		Sort:       uwg.AnalysisSort(values.Get("sort")),
		Pagination: httpx.ParsePagination(values),
	})
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	writeGroupPage(w, page)
}

// @Summary		Criar uma análise de grupo
// @Description	Análise de IA de um grupo acompanhado. Com `message_count`, roda a cada `messagesPerCycle` mensagens novas; com `scheduled`, uma vez por dia no horário do workspace, sobre o que chegou desde a última. Cada ciclo consome saldo e não roda sem saldo. Começa das mensagens que chegarem depois da criação.
// @Tags			Unofficial WhatsApp Groups
// @Accept			json
// @Produce		json
// @Param			body	body		analysisRequest	true	"Análise"
// @Success		201		{object}	unofficial_whatsapp_group.Analysis
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/analyses [post]
func (h *WorkspaceGroupHandler) CreateAnalysis(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req analysisRequest
	if !decodeGroupBody(w, r, &req) {
		return
	}
	analysis, err := h.groups.CreateAnalysis(r.Context(), a, uwguc.AnalysisInput{
		GroupID: req.GroupID, Name: req.Name, Prompt: req.Prompt, Trigger: req.Trigger,
		MessagesPerCycle: req.MessagesPerCycle, ScheduleHour: req.ScheduleHour, ScheduleMinute: req.ScheduleMinute,
		TrackedFields: req.TrackedFields,
	})
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, analysis)
}

// @Summary		Detalhe de uma análise de grupo
// @Description	Análise com o grupo, a visão geral dos últimos 30 dias (sentimento por ciclo, temas, menções, participantes, alertas, campos acompanhados) e, para cada participante que está em dois ou mais grupos do escopo, esses grupos.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			id		path		string	true	"Análise"
// @Param			days	query		int		false	"Janela da visão geral em dias; 30 por padrão, -1 para todos os ciclos"
// @Success		200	{object}	unofficial_whatsapp_group_usecase.AnalysisDetail
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/analyses/{id} [get]
func (h *WorkspaceGroupHandler) GetAnalysis(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	days := 0
	if v := httpx.ParseIntQuery(r.URL.Query().Get("days")); v != nil {
		days = *v
	}
	detail, err := h.groups.GetAnalysis(r.Context(), a, mux.Vars(r)["id"], days)
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, detail)
}

// @Summary		Editar uma análise de grupo
// @Description	Altera nome, foco, status (pausar ou retomar), gatilho, horário e campos acompanhados. Mudar o gatilho ou o horário reagenda a próxima execução.
// @Tags			Unofficial WhatsApp Groups
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"Análise"
// @Param			body	body		analysisPatchRequest	true	"Campos a alterar"
// @Success		200		{object}	unofficial_whatsapp_group.Analysis
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/analyses/{id} [patch]
func (h *WorkspaceGroupHandler) UpdateAnalysis(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req analysisPatchRequest
	if !decodeGroupBody(w, r, &req) {
		return
	}
	analysis, err := h.groups.UpdateAnalysis(r.Context(), a, mux.Vars(r)["id"], uwguc.AnalysisPatch{
		Name: req.Name, Prompt: req.Prompt, Status: req.Status, Trigger: req.Trigger,
		MessagesPerCycle: req.MessagesPerCycle, ScheduleHour: req.ScheduleHour, ScheduleMinute: req.ScheduleMinute,
		TrackedFields: req.TrackedFields,
	})
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, analysis)
}

// @Summary		Excluir uma análise de grupo
// @Description	Exclui a análise e o histórico dos ciclos.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			id	path		string	true	"Análise"
// @Success		200	{object}	map[string]bool
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/analyses/{id} [delete]
func (h *WorkspaceGroupHandler) DeleteAnalysis(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	if err := h.groups.DeleteAnalysis(r.Context(), a, mux.Vars(r)["id"]); err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]bool{"deleted": true})
}

// @Summary		Ciclos de uma análise de grupo
// @Description	Ciclos da análise, do mais recente ao mais antigo, com o resultado completo, o período analisado, os tokens usados e o motivo de falha quando houver.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			id			path		string	true	"Análise"
// @Param			page		query		int		false	"Página"
// @Param			pageSize	query		int		false	"Itens por página"
// @Success		200			{object}	runHistoryResponse
// @Failure		403			{object}	response.ErrorResponse
// @Failure		404			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/analyses/{id}/runs [get]
func (h *WorkspaceGroupHandler) AnalysisRuns(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	history, err := h.groups.AnalysisRuns(r.Context(), a, mux.Vars(r)["id"], httpx.ParsePagination(r.URL.Query()))
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	runs := history.Runs.Items
	if runs == nil {
		runs = []*uwg.Run{}
	}
	response.WriteSuccess(w, http.StatusOK, runHistoryResponse{
		Data:              runs,
		Meta:              response.PaginationMeta{Page: history.Runs.Page, PageSize: history.Runs.PageSize, TotalPages: history.Runs.TotalPages, TotalItems: history.Runs.TotalItems},
		ParticipantGroups: history.ParticipantGroups,
	})
}

// @Summary		Mensagens de um ciclo
// @Description	Mensagens do período que o ciclo analisou, 100 por vez a partir de `offset`, lidas pela conversa do grupo que quem consulta pode abrir.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			id		path		string	true	"Análise"
// @Param			runId	path		string	true	"Ciclo"
// @Param			offset	query		int		false	"Deslocamento"
// @Success		200		{array}		unofficial_whatsapp_group.TranscriptLine
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/analyses/{id}/runs/{runId}/messages [get]
func (h *WorkspaceGroupHandler) RunMessages(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	offset := 0
	if v := httpx.ParseIntQuery(r.URL.Query().Get("offset")); v != nil && *v > 0 {
		offset = *v
	}
	vars := mux.Vars(r)
	lines, err := h.groups.RunMessages(r.Context(), a, vars["id"], vars["runId"], offset)
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, lines)
}
