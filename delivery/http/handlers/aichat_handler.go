package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/aichat"
	copilot_domain "vozko/domain/copilot"
	"vozko/infra/http/middleware"
	"vozko/usecases/agentloop"
	aichat_usecase "vozko/usecases/aichat"
	copilot_usecase "vozko/usecases/copilot"
)

const (
	chatDefaultPageSize = 30
	chatMaxPageSize     = 100
)

type AIChatHandler struct {
	svc     *aichat_usecase.Service
	copilot *copilot_usecase.Service
}

func NewAIChatHandler(svc *aichat_usecase.Service, copilot *copilot_usecase.Service) *AIChatHandler {
	return &AIChatHandler{svc: svc, copilot: copilot}
}

type threadDTO struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Model         string  `json:"model"`
	LastMessageAt *string `json:"lastMessageAt,omitempty"`
	CreatedAt     string  `json:"createdAt"`
}

type toolActivityDTO struct {
	Name    string                     `json:"name"`
	Summary string                     `json:"summary"`
	Ok      bool                       `json:"ok"`
	Chart   *copilot_domain.Chart      `json:"chart,omitempty"`
	Card    *copilot_domain.ActionCard `json:"card,omitempty"`
	Media   *copilot_domain.Media      `json:"image,omitempty"`
}

type messageDTO struct {
	ID          string                      `json:"id"`
	Role        string                      `json:"role"`
	Content     string                      `json:"content"`
	Model       string                      `json:"model,omitempty"`
	Reasoning   string                      `json:"reasoning,omitempty"`
	Tools       []toolActivityDTO           `json:"tools,omitempty"`
	Attachments []copilot_domain.Attachment `json:"attachments,omitempty"`
	Proposal    *proposalDTO                `json:"proposal,omitempty"`
	CreatedAt   string                      `json:"createdAt"`
}

type proposalDTO struct {
	ID       string                       `json:"id"`
	ToolName string                       `json:"toolName"`
	Fields   []copilot_domain.Field       `json:"fields"`
	Preview  *copilot_domain.Preview      `json:"preview,omitempty"`
	Secrets  []copilot_domain.SecretField `json:"secrets,omitempty"`
	Choices  []copilot_domain.ChoiceField `json:"choices,omitempty"`
	Status   string                       `json:"status"`
}

func toProposalDTO(m *aichat.Message) *proposalDTO {
	if m.ProposalID == "" {
		return nil
	}
	var pa copilot_domain.PendingAction
	if json.Unmarshal(m.Proposal, &pa) != nil {
		return nil
	}
	return &proposalDTO{ID: pa.ID, ToolName: pa.ToolName, Fields: pa.Fields, Preview: pa.Preview, Secrets: pa.Secrets, Choices: pa.Choices, Status: string(m.ProposalStatus)}
}

func (h *AIChatHandler) CreateThread(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)

	var body struct {
		Model string `json:"model"`
		Title string `json:"title"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	thread, err := h.svc.CreateThread(workspaceID, claims.UserID, body.Model, body.Title)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toThreadDTO(thread))
}

func (h *AIChatHandler) ListThreads(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	limit, offset, page, pageSize := parseChatPagination(r)

	threads, total, err := h.svc.ListThreads(workspaceID, claims.UserID, limit, offset)
	if err != nil {
		h.writeError(w, err)
		return
	}

	items := make([]threadDTO, len(threads))
	for i, t := range threads {
		items[i] = toThreadDTO(t)
	}
	response.WriteSuccess(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": page, "pageSize": pageSize,
	})
}

func (h *AIChatHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	threadID := mux.Vars(r)["id"]
	limit, offset, page, pageSize := parseChatPagination(r)

	msgs, total, err := h.svc.ListMessages(workspaceID, claims.UserID, threadID, limit, offset)
	if err != nil {
		h.writeError(w, err)
		return
	}

	items := make([]messageDTO, len(msgs))
	for i, m := range msgs {
		items[i] = toMessageDTO(m)
	}
	response.WriteSuccess(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": page, "pageSize": pageSize, "running": h.copilot.TurnRunning(threadID),
	})
}

func (h *AIChatHandler) RenameThread(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	threadID := mux.Vars(r)["id"]

	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, "corpo inválido", nil)
		return
	}
	if err := h.svc.RenameThread(workspaceID, claims.UserID, threadID, body.Title); err != nil {
		h.writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *AIChatHandler) DeleteThread(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	threadID := mux.Vars(r)["id"]

	if err := h.svc.DeleteThread(workspaceID, claims.UserID, threadID); err != nil {
		h.writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]any{"ok": true})
}

// @Summary		Enviar uma mensagem para a Elo
// @Description	Envia a mensagem e responde com o stream da resposta (`text/event-stream`): trechos de texto e de raciocínio, passos de ferramenta, cartões, `tool_proposal` com a proposta que espera aprovação e, no fim, `done`, `error` ou `awaiting_approval`.
// @Description
// @Description	| Campo | Tipo | Regra |
// @Description	|---|---|---|
// @Description	| content | string | o texto do usuário; vazio só com anexos |
// @Description	| attachments | string[] | ids de mídia anexados |
// @Description	| model | string | modelo da conversa, opcional |
// @Description	| timezone | string | fuso horário do usuário |
// @Description	| mode | string | `ask` (padrão), `edit` ou `full` |
// @Description	| view | object | a tela aberta; vazio fora das telas abaixo |
// @Description
// @Description	`view.surface` é `attendance` (com `dateFrom`, `dateTo` em YYYY-MM-DD, `departmentId`, `memberId`, `channel`, `campaignId`, `campaignType`, `includeAi`), `studio` (com `projectId` e `projectKind` `image` ou `video`) ou `leads`. Na página Leads, `view.leadFilter` é o filtro efetivo da lista, no mesmo formato do parâmetro `filter` de GET /leads (`{groups: [{conjunction, predicates: [{field, key, operator, values}]}]}`, com a busca incluída como predicado `query`), até 16 KiB e validado como os filtros de lead; `view.selectedLeads` é quantos leads estão marcados na tabela (0 a 10.000.000). A Elo recebe só os nomes dos campos filtrados, nunca os valores, e usa o filtro quando uma ferramenta pede `use_screen_filter`. Campos de outra tela, filtro inválido ou contagem fora do intervalo respondem 400.
// @Description
// @Description	O idioma do pedido (parâmetro `locale` ou cabeçalho `Accept-Language`: pt, en, es ou de; pt quando ausente) é o idioma do que a Elo grava para o usuário, como a exportação de leads.
// @Description
// @Description	Propostas de ações sobre leads (`prepare_lead_action`, `start_lead_send`, `cancel_lead_send`) trazem `preview.kind` `lead_action` com `data` `{stage: prepare|start|cancel, action, mode, previewId, matched, selected, eligible, partial, skipped: {motivo: n}, counted: {motivo: n}, quote, parts}`; `selected` é o tamanho confirmado da seleção (o que a aprovação executa), e com `partial` verdadeiro `eligible` e `skipped` ainda estão sendo contados: o resultado final sai de GET /leads/actions/previews/{previewId}; `quote` é a cotação do envio (`count`, `parts`, `splitRequired`, `unitPriceMicros`, `costMicros`, `balanceMicros`, `currency`, `affordable`, `capRemaining`, `fits`, `refusal`, `dailyCap`, `estimatedDays`) e `parts` são as campanhas preparadas (`campaignId`, `name`, `status`, `entries`, `eligible`).
// @Tags			Elo
// @Accept			json
// @Produce		text/event-stream
// @Param			id		path		string	true	"conversa"
// @Param			body	body		object	true	"mensagem e tela aberta (campos na tabela acima)"
// @Success		200		{string}	string	"eventos SSE"
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/chat/threads/{id}/messages [post]
func (h *AIChatHandler) StreamMessage(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	threadID := mux.Vars(r)["id"]

	var body struct {
		Content     string              `json:"content"`
		Attachments []string            `json:"attachments"`
		Model       string              `json:"model"`
		View        copilot_domain.View `json:"view"`
		Timezone    string              `json:"timezone"`
		Mode        string              `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, "corpo inválido", nil)
		return
	}
	if err := body.View.Validate(); err != nil {
		response.WriteError(w, http.StatusBadRequest, "contexto de tela inválido", nil)
		return
	}
	mode, err := copilot_domain.ParseMode(body.Mode)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "modo de execução inválido", nil)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		response.WriteError(w, http.StatusInternalServerError, "streaming não suportado", nil)
		return
	}

	thread, err := h.svc.Precheck(workspaceID, claims.UserID, threadID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if m := strings.TrimSpace(body.Model); m != "" {
		thread.Model = m
	}

	r = withDepartmentCreationScope(r, "")
	turn, err := h.copilot.BeginTurn(r.Context(), thread.ID, claims.UserID)
	if err != nil {
		h.writeTurnError(w, err)
		return
	}
	cc := copilotCtx(r, claims.UserID, workspaceID)
	cc.View = body.View
	cc.Timezone = body.Timezone
	cc.Mode = mode
	message := copilot_domain.UserMessage{Content: body.Content, AttachmentIDs: body.Attachments}
	streamTurn(w, r, flusher, turn, func(ctx context.Context, emit agentloop.Emit) error {
		return h.copilot.Stream(ctx, thread, message, cc, emit)
	})
}

type approveActionRequest struct {
	Secrets map[string]string   `json:"secrets"`
	Choices map[string]string   `json:"choices"`
	View    copilot_domain.View `json:"view"`
	Mode    string              `json:"mode"`
}

// @Summary		Aprovar uma proposta da Elo
// @Description	Executa a proposta aprovada e continua a resposta no stream (`text/event-stream`). Antes de executar, a verificação da proposta roda de novo; numa ação sobre leads, a contagem mostrada no cartão é a que vale, cada cartão executa uma única vez e uma seleção que mudou desde o cartão não é executada. O idioma do pedido segue a mesma regra do envio de mensagem.
// @Description
// @Description	| Campo | Tipo | Regra |
// @Description	|---|---|---|
// @Description	| secrets | object | campos protegidos digitados no cartão |
// @Description	| choices | object | escolhas feitas no cartão |
// @Description	| view | object | a tela aberta, no mesmo formato do envio de mensagem |
// @Description	| mode | string | `ask` (padrão), `edit` ou `full` |
// @Tags			Elo
// @Accept			json
// @Produce		text/event-stream
// @Param			id			path		string	true	"conversa"
// @Param			actionId	path		string	true	"id da proposta"
// @Param			body		body		object	false	"campos na tabela acima"
// @Success		200			{string}	string	"eventos SSE"
// @Failure		400			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Failure		409			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/chat/threads/{id}/actions/{actionId}/approve [post]
func (h *AIChatHandler) ApproveAction(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	threadID := mux.Vars(r)["id"]
	actionID := mux.Vars(r)["actionId"]

	flusher, ok := w.(http.Flusher)
	if !ok {
		response.WriteError(w, http.StatusInternalServerError, "streaming não suportado", nil)
		return
	}
	var body approveActionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		response.WriteError(w, http.StatusBadRequest, "corpo inválido", nil)
		return
	}
	if err := body.View.Validate(); err != nil {
		response.WriteError(w, http.StatusBadRequest, "contexto de tela inválido", nil)
		return
	}
	mode, err := copilot_domain.ParseMode(body.Mode)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "modo de execução inválido", nil)
		return
	}
	thread, err := h.svc.Precheck(workspaceID, claims.UserID, threadID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	r = withDepartmentCreationScope(r, "")
	turn, err := h.copilot.BeginTurn(r.Context(), thread.ID, claims.UserID)
	if err != nil {
		h.writeTurnError(w, err)
		return
	}
	cc := copilotCtx(r, claims.UserID, workspaceID)
	cc.View = body.View
	cc.Mode = mode
	approval := copilot_domain.Approval{Secrets: body.Secrets, Choices: body.Choices}
	streamTurn(w, r, flusher, turn, func(ctx context.Context, emit agentloop.Emit) error {
		return h.copilot.Approve(ctx, thread, actionID, approval, cc, emit)
	})
}

func (h *AIChatHandler) RejectAction(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	threadID := mux.Vars(r)["id"]
	actionID := mux.Vars(r)["actionId"]

	flusher, ok := w.(http.Flusher)
	if !ok {
		response.WriteError(w, http.StatusInternalServerError, "streaming não suportado", nil)
		return
	}
	thread, err := h.svc.Precheck(workspaceID, claims.UserID, threadID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	emit := startChatSSE(w, flusher)
	if err := h.copilot.Reject(r.Context(), thread, actionID, emit); err != nil {
		emit("error", map[string]any{"error": err.Error()})
	}
}

// @Summary		Responder a um comando da Elo na tela aberta
// @Description	Quando a Elo trabalha no Estúdio, ela envia pelo stream da conversa um evento `screen_command` (`{id, name, projectId, args}`), com `name` em `read`, `edit`, `look`, `resolve_source` ou `follow_job`. O editor aberto executa o comando e devolve o resultado aqui, uma única vez, dentro do tempo do comando; depois disso a resposta é ignorada e a Elo é avisada de que o editor não respondeu.
// @Description
// @Description	| Campo | Tipo | Regra |
// @Description	|---|---|---|
// @Description	| ok | boolean | true quando o comando foi aplicado |
// @Description	| data | object | resultado do comando (resumo do projeto, ids criados, estado dos trabalhos) |
// @Description	| error | object | `{code, message}` quando ok é false; code em snake_case, message com até 600 caracteres |
// @Description	| images | string[] | no máximo 1 imagem JPEG ou PNG em data URL, até 1,5 MB |
// @Description
// @Description	Só o dono da conversa responde; campos desconhecidos e corpos acima de 2 MB são recusados.
// @Tags			Elo
// @Accept			json
// @Produce		json
// @Param			id			path		string						true	"conversa"
// @Param			commandId	path		string						true	"id do comando recebido no evento screen_command"
// @Param			body		body		object						true	"resultado do editor (campos na tabela acima)"
// @Success		202			{object}	map[string]bool
// @Failure		400			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Failure		413			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/chat/threads/{id}/screen/{commandId} [post]
func (h *AIChatHandler) ScreenReply(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	threadID := mux.Vars(r)["id"]
	commandID := mux.Vars(r)["commandId"]
	if _, err := uuid.Parse(commandID); err != nil || claims == nil {
		response.WriteError(w, http.StatusBadRequest, "comando inválido", nil)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, copilot_domain.MaxScreenReplyBytes))
	if err != nil {
		response.WriteError(w, http.StatusRequestEntityTooLarge, "resposta grande demais", nil)
		return
	}
	reply, err := copilot_domain.DecodeScreenReply(raw)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "resposta inválida", nil)
		return
	}
	thread, err := h.svc.Authorize(workspaceID, claims.UserID, threadID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if err := h.copilot.DeliverScreenReply(thread.ID, commandID, reply); err != nil {
		response.WriteError(w, http.StatusServiceUnavailable, "a Elo não está esperando esta resposta", nil)
		return
	}
	response.WriteSuccess(w, http.StatusAccepted, map[string]bool{"ok": true})
}

// @Summary		Acompanhar a resposta da Elo em andamento
// @Description	Reabre o stream de uma resposta que ainda está rodando nesta conversa, por exemplo depois de recarregar a página. O stream (`text/event-stream`) primeiro repete o que a resposta já produziu, com os trechos de texto e de raciocínio já agrupados, e os comandos `screen_command` ainda sem resposta; depois segue os eventos novos até `done`, `error` ou `awaiting_approval`, no mesmo formato do envio de mensagem.
// @Description
// @Description	Uma resposta que terminou continua visível aqui por alguns segundos e depois some; o histórico da conversa passa a ter a mensagem completa. O histórico (`GET /chat/threads/{id}/messages`) informa `running: true` enquanto há resposta em andamento.
// @Description
// @Description	Só o dono da conversa acompanha. Sem resposta em andamento, responde 404 com `code: turn_not_running`.
// @Tags			Elo
// @Produce		text/event-stream
// @Param			id	path		string	true	"conversa"
// @Success		200	{string}	string	"eventos SSE"
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/chat/threads/{id}/turn/events [get]
func (h *AIChatHandler) ObserveTurn(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	threadID := mux.Vars(r)["id"]

	flusher, ok := w.(http.Flusher)
	if !ok {
		response.WriteError(w, http.StatusInternalServerError, "streaming não suportado", nil)
		return
	}
	thread, err := h.svc.Authorize(workspaceID, claims.UserID, threadID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	turn, err := h.copilot.ObserveTurn(thread.ID, claims.UserID)
	if err != nil {
		h.writeTurnError(w, err)
		return
	}
	replay, live, leave := turn.Subscribe()
	defer leave()
	pumpTurn(r.Context(), startChatSSE(w, flusher), replay, live)
}

// @Summary		Parar a resposta da Elo em andamento
// @Description	Encerra a resposta que está rodando nesta conversa. O que já foi feito fica salvo no histórico e quem acompanha o stream recebe o fim normalmente. Fechar o stream não para a resposta: sem ninguém acompanhando, ela só segue por um curto período antes de ser encerrada.
// @Description
// @Description	Só o dono da conversa para. Sem resposta em andamento, responde 404 com `code: turn_not_running`.
// @Tags			Elo
// @Produce		json
// @Param			id	path		string	true	"conversa"
// @Success		202	{object}	map[string]bool
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/chat/threads/{id}/turn/stop [post]
func (h *AIChatHandler) StopTurn(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	threadID := mux.Vars(r)["id"]

	thread, err := h.svc.Authorize(workspaceID, claims.UserID, threadID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if err := h.copilot.StopTurn(thread.ID, claims.UserID); err != nil {
		h.writeTurnError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusAccepted, map[string]bool{"ok": true})
}

type ThreadUsageResponse struct {
	Available        bool  `json:"available" example:"true"`
	Calls            int   `json:"calls" example:"15"`
	InputTokens      int64 `json:"inputTokens" example:"960532"`
	OutputTokens     int64 `json:"outputTokens" example:"19172"`
	CacheReadTokens  int64 `json:"cacheReadTokens" example:"820000"`
	CacheWriteTokens int64 `json:"cacheWriteTokens" example:"95000"`
	ReasoningTokens  int64 `json:"reasoningTokens" example:"3100"`
}

type ThreadCostResponse struct {
	Available    bool                `json:"available" example:"true"`
	AmountMicros int64               `json:"amountMicros" example:"420000"`
	Currency     string              `json:"currency" example:"BRL"`
	Usage        ThreadUsageResponse `json:"usage"`
}

// @Summary		Custo da conversa com a Elo
// @Description	Quanto foi debitado do saldo do workspace por esta conversa, do início até agora, já com o preço do plano aplicado (nunca o custo do provedor). Soma o uso do modelo em cada resposta e as gerações de imagem, música, locução, vídeo e trabalhos do Estúdio que a Elo iniciou nesta conversa, menos estornos. Cada débito é convertido pela cotação registrada no momento da cobrança.
// @Description
// @Description	`amountMicros` é o valor em milionésimos da moeda de cobrança (`currency`, hoje BRL). Os débitos chegam alguns segundos depois de cada chamada, então logo após uma resposta o total pode ainda subir.
// @Description
// @Description	`available: false` quando o total não pode ser exato: conversas anteriores ao registro de custo por conversa ou débito sem cotação. Nesse caso `amountMicros` vem 0 e não deve ser mostrado como valor.
// @Description
// @Description	`usage` traz os tokens das chamadas ao modelo nesta conversa: `inputTokens` (já inclui os lidos do cache), `cacheReadTokens` (entrada reaproveitada do cache, cobrada bem mais barata), `cacheWriteTokens` (entrada gravada no cache), `outputTokens` (já inclui o raciocínio), `reasoningTokens` e `calls` (quantas chamadas ao modelo; gerações de mídia não contam). `usage.available: false` quando algum débito da conversa não tem o registro de tokens correspondente; nesse caso os números vêm 0 e não devem ser mostrados.
// @Description
// @Description	Só o dono da conversa consulta; a conversa de outra pessoa responde 404.
// @Tags			Elo
// @Produce		json
// @Param			id	path		string	true	"conversa"
// @Success		200	{object}	ThreadCostResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/chat/threads/{id}/cost [get]
func (h *AIChatHandler) ThreadCost(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	workspaceID := middleware.GetWorkspaceID(r)
	threadID := mux.Vars(r)["id"]

	cost, err := h.svc.ThreadCost(workspaceID, claims.UserID, threadID)
	switch {
	case errors.Is(err, aichat.ErrThreadNotFound), errors.Is(err, aichat_usecase.ErrForbidden):
		response.WriteError(w, http.StatusNotFound, "conversa não encontrada", nil)
		return
	case err != nil:
		h.writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, threadCostResponse(cost))
}

func threadCostResponse(cost aichat.ThreadCost) ThreadCostResponse {
	tokens := cost.Usage.Tokens
	return ThreadCostResponse{
		Available:    cost.Available,
		AmountMicros: cost.AmountMicros,
		Currency:     cost.Currency,
		Usage: ThreadUsageResponse{
			Available:        cost.Usage.Available,
			Calls:            cost.Usage.Calls,
			InputTokens:      tokens.Input,
			OutputTokens:     tokens.Output,
			CacheReadTokens:  tokens.CacheRead,
			CacheWriteTokens: tokens.CacheWrite,
			ReasoningTokens:  tokens.Reasoning,
		},
	}
}

func streamTurn(w http.ResponseWriter, r *http.Request, flusher http.Flusher, turn *copilot_usecase.Turn, run func(context.Context, agentloop.Emit) error) {
	replay, live, leave := turn.Subscribe()
	defer leave()
	turn.Run(run)
	pumpTurn(r.Context(), startChatSSE(w, flusher), replay, live)
}

func pumpTurn(ctx context.Context, emit func(string, interface{}), replay []copilot_domain.TurnEvent, live <-chan copilot_domain.TurnEvent) {
	for _, ev := range replay {
		emit(ev.Type, ev.Payload)
	}
	for {
		select {
		case ev, ok := <-live:
			if !ok {
				return
			}
			emit(ev.Type, ev.Payload)
		case <-ctx.Done():
			return
		}
	}
}

func (h *AIChatHandler) writeTurnError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, copilot_domain.ErrTurnRunning):
		response.WriteErrorWithCode(w, http.StatusConflict, "turn_running", "a Elo ainda está respondendo nesta conversa", nil)
	case errors.Is(err, copilot_domain.ErrTurnNotRunning):
		response.WriteErrorWithCode(w, http.StatusNotFound, "turn_not_running", "nenhuma resposta da Elo está em andamento nesta conversa", nil)
	case errors.Is(err, copilot_domain.ErrTurnForbidden):
		response.WriteError(w, http.StatusForbidden, "sem acesso a esta conversa", nil)
	default:
		h.writeError(w, err)
	}
}

func copilotCtx(r *http.Request, userID, workspaceID string) copilot_domain.Context {
	claims := middleware.GetClaims(r)
	return copilot_domain.Context{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Departments: middleware.GetDepartmentFilter(r),
		SystemAdmin: claims != nil && claims.Role == "admin",
		Locale:      httpx.RequestLocale(r),
	}
}

func startChatSSE(w http.ResponseWriter, flusher http.Flusher) func(string, interface{}) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return func(eventType string, payload interface{}) {
		data, err := json.Marshal(map[string]interface{}{"type": eventType, "payload": payload})
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

func (h *AIChatHandler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, aichat.ErrThreadNotFound):
		response.WriteError(w, http.StatusNotFound, "conversa não encontrada", nil)
	case errors.Is(err, aichat_usecase.ErrForbidden):
		response.WriteError(w, http.StatusForbidden, "sem acesso a esta conversa", nil)
	case errors.Is(err, aichat_usecase.ErrNoSubscription):
		response.WriteError(w, http.StatusPaymentRequired, "plano ativo necessário", nil)
	case errors.Is(err, aichat_usecase.ErrInsufficientBalance):
		response.WriteError(w, http.StatusPaymentRequired, "saldo insuficiente", nil)
	case errors.Is(err, aichat_usecase.ErrEmptyMessage):
		response.WriteError(w, http.StatusBadRequest, "mensagem vazia", nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "erro interno", nil)
	}
}

func parseChatPagination(r *http.Request) (limit, offset, page, pageSize int) {
	page = 1
	pageSize = chatDefaultPageSize
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := r.URL.Query().Get("pageSize"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pageSize = n
		}
	}
	if pageSize > chatMaxPageSize {
		pageSize = chatMaxPageSize
	}
	return pageSize, (page - 1) * pageSize, page, pageSize
}

func toThreadDTO(t *aichat.Thread) threadDTO {
	dto := threadDTO{
		ID:        t.ID,
		Title:     t.Title,
		Model:     t.Model,
		CreatedAt: t.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if t.LastMessageAt != nil {
		s := t.LastMessageAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		dto.LastMessageAt = &s
	}
	return dto
}

func toMessageDTO(m *aichat.Message) messageDTO {
	dto := messageDTO{
		ID:        m.ID,
		Role:      string(m.Role),
		Content:   m.Content,
		Model:     m.Model,
		CreatedAt: m.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if len(m.Reasoning) > 0 {
		var reasoning string
		if json.Unmarshal(m.Reasoning, &reasoning) == nil {
			dto.Reasoning = reasoning
		}
	}
	if len(m.ToolCalls) > 0 {
		_ = json.Unmarshal(m.ToolCalls, &dto.Tools)
	}
	if len(m.Attachments) > 0 {
		_ = json.Unmarshal(m.Attachments, &dto.Attachments)
	}
	dto.Proposal = toProposalDTO(m)
	return dto
}
