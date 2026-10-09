package calllisthttp

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/calls/calllist"
	calllist_usecase "vozko/usecases/calls/calllist"
)

type Service interface {
	Lists(ctx context.Context, a calllist_usecase.Actor, q calllist.ListQuery) (calllist_usecase.ListViews, error)
	View(ctx context.Context, a calllist_usecase.Actor, id string) (calllist_usecase.ListView, error)
	Update(ctx context.Context, a calllist_usecase.Actor, id string, c calllist.Change) (calllist_usecase.ListView, error)
	Delete(ctx context.Context, a calllist_usecase.Actor, id string) error
	Items(ctx context.Context, a calllist_usecase.Actor, q calllist.ItemQuery) (calllist_usecase.ItemRows, error)
	Next(ctx context.Context, a calllist_usecase.Actor, listID string) (*calllist_usecase.NextResult, error)
	Release(ctx context.Context, a calllist_usecase.Actor, itemID string) (calllist_usecase.ItemVerdict, error)
	Close(ctx context.Context, a calllist_usecase.Actor, itemID string, c calllist.Closing) (calllist_usecase.ItemVerdict, error)
}

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

const codeStatusInvalid = "call_list_status_invalid"

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (calllist_usecase.Actor, bool) {
	if h.service == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, calllist.ErrorCode(calllist.ErrUnavailable), "As listas de ligação não estão disponíveis neste servidor", nil)
		return calllist_usecase.Actor{}, false
	}
	actor, ok := httpx.WorkspaceActor(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "authentication required", nil)
		return calllist_usecase.Actor{}, false
	}
	return actor, true
}

func writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	if status, code, ok := Refusal(err); ok {
		response.WriteErrorWithCode(w, status, code, err.Error(), nil)
		return
	}
	response.WriteError(w, http.StatusInternalServerError, "Failed to handle the call list", nil)
}

func positiveQuery(r *http.Request, name string) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	return value, err == nil && value >= 0
}

func instantQuery(r *http.Request, name string) (*time.Time, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return nil, true
	}
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, false
	}
	value = value.UTC()
	return &value, true
}

// @Summary		Listas de ligação
// @Description	Lista as listas de ligação do workspace, das mais novas para as mais antigas, com o andamento de cada uma: `selected` (leads da seleção), `itemCount` (leads que entraram na lista), `closedCount` (itens com resultado), `openCount` (itens que faltam) e `skipped` (leads que ficaram de fora, por motivo: `blocked`, `opted_out`, `no_number`, `number_not_held`, `invalid_number`, `gone`), `calledCount` (itens que já receberam ao menos uma ligação feita pela lista) e `callbackCount` (itens esperando um retorno marcado com `_callback`). Cada lista traz também as decisões do servidor que a tela mostra: `acceptsOutcomes` (a lista aceita resultados, `active` ou `paused`) e `statusMoves` (os status para os quais você pode mudá-la; vazio para quem não gerencia listas). Quem gerencia listas (`call_lists:manage`) vê todas; os demais veem só as listas em que foram escolhidos para ligar. Uma lista nasce com `status` `building` enquanto os itens são montados em segundo plano e passa a `active`, ou a `failed` com `failureCode` (`no_callable_lead` quando nenhum lead da seleção pode receber ligações, `build_failed` quando a montagem falhou várias vezes). Listas são criadas a partir de uma seleção de leads em POST /leads/actions com a ação `call_list`.
// @Tags			Listas de ligação
// @Produce		json
// @Param			status		query		string	false	"Filtra por status"	Enums(building, active, paused, archived, failed)
// @Param			page		query		int		false	"Página, a partir de 1"
// @Param			pageSize	query		int		false	"Itens por página, até 100"
// @Success		200			{object}	CallListPageResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Failure		503			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-lists [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	status := calllist.Status(strings.TrimSpace(r.URL.Query().Get("status")))
	if status != "" && !status.Valid() {
		response.WriteErrorWithCode(w, http.StatusBadRequest, codeStatusInvalid, "status must be building, active, paused, archived or failed", nil)
		return
	}
	paging := httpx.ParsePagination(r.URL.Query())
	q := calllist.ListQuery{Status: status, Page: paging.Page, PageSize: paging.PageSize}.Normalized()
	page, err := h.service.Lists(r.Context(), actor, q)
	if err != nil {
		writeError(w, err)
		return
	}
	out := CallListPageResponse{Items: make([]CallListResponse, 0, len(page.Lists)), Total: page.Total, Page: q.Page, PageSize: q.PageSize}
	for _, view := range page.Lists {
		out.Items = append(out.Items, ViewResponseOf(view))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Uma lista de ligação
// @Description	Devolve a lista com o andamento (veja GET /call-lists). Só para quem foi escolhido para ligar nela ou gerencia listas; para os demais responde 404.
// @Tags			Listas de ligação
// @Produce		json
// @Param			listId	path		string	true	"ID da lista"
// @Success		200		{object}	CallListResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-lists/{listId} [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	view, err := h.service.View(r.Context(), actor, mux.Vars(r)["listId"])
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, ViewResponseOf(view))
}

// @Summary		Alterar uma lista de ligação
// @Description	Renomeia a lista, troca quem liga (`assigneeIds`, cada membro precisa poder trabalhar em listas: `call_lists:read`, `sip_trunks:read`, `sip_trunks:call` e `call_session:use`, senão 422 `call_list_assignee_cannot_work`) ou muda o status: `active` e `paused` alternam, as duas vão para `archived`, e `archived` volta para `active` (outra mudança responde 409 `call_list_status_transition`). Ao trocar quem liga, só os membros novos são conferidos. Uma lista em montagem responde 409 `call_list_building`. Só os campos enviados mudam. Exige `call_lists:manage`.
// @Tags			Listas de ligação
// @Accept			json
// @Produce		json
// @Param			listId	path		string					true	"ID da lista"
// @Param			body	body		UpdateCallListRequest	true	"Campos a alterar"
// @Success		200		{object}	CallListResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-lists/{listId} [patch]
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var body UpdateCallListRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	change := calllist.Change{Name: body.Name, AssigneeIDs: body.AssigneeIDs}
	if body.Status != nil {
		status := calllist.Status(strings.TrimSpace(*body.Status))
		change.Status = &status
	}
	view, err := h.service.Update(r.Context(), actor, mux.Vars(r)["listId"], change)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, ViewResponseOf(view))
}

// @Summary		Excluir uma lista de ligação
// @Description	Exclui a lista e todos os seus itens. As ligações feitas continuam no histórico de chamadas. Exige `call_lists:manage`.
// @Tags			Listas de ligação
// @Param			listId	path	string	true	"ID da lista"
// @Success		204
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-lists/{listId} [delete]
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), actor, mux.Vars(r)["listId"]); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Itens de uma lista de ligação
// @Description	Lista os itens em páginas: envie `after` com o `next` da página anterior. Os pendentes (`state=pending`) vêm na ordem em que a fila os serve: primeiro os retornos já vencidos (`callbackAt` até o instante da primeira página), do mais antigo para o mais novo; depois a fila pela posição; por fim os retornos ainda por vir, pelo horário. A primeira página pendente responde `asOf`, o instante em que a agenda foi cortada, e, quando há mais, `next` com `nextAt` (o `callbackAt` do último item, ausente na fila comum); a página seguinte envia `after`, `afterAt` e `asOf` como vieram, e sem `asOf` responde 400 `call_list_items_cursor_invalid`. A página seguinte nunca pula nem repete um item por causa de uma mudança no último item da página anterior. Os outros filtros seguem a posição na fila. Cada item traz o nome do lead (`leadName`, ausente quando o nome guardado é só o número dele), o bairro e a cidade do endereço principal dele (`leadDistrict`, `leadCity`), o número a chamar, o estado (`pending`, `reserved`, `closed`), quem o reservou e até quando, o resultado escolhido (`disposition`, um código do catálogo de resultados do workspace, `_callback` para ligar de novo em `callbackAt` ou `_refused` quando o lead deixou de poder receber ligações, com o motivo em `refusal`), a nota, a última ligação (`lastCallId`) e, tirados do histórico de chamadas, o resultado técnico dela (`outcome`) e quantas ligações feitas para o lead (só as de saída, de qualquer origem) existem desde que ele entrou na lista (`attempts`). `closable` diz se você pode registrar o resultado do item agora (veja POST /call-list-items/{itemId}/close): a última ligação registrada no item é sua, para o lead do item, o item não está fechado nem esperando um retorno sem nova ligação, ninguém mais o pegou, a lista aceita resultados e você pode trabalhar em listas (`call_lists:read`, `sip_trunks:read`, `sip_trunks:call` e `call_session:use`). Para quem foi escolhido para ligar na lista ou gerencia listas.
// @Tags			Listas de ligação
// @Produce		json
// @Param			listId	path		string	true	"ID da lista"
// @Param			state	query		string	false	"Filtra por estado"	Enums(pending, reserved, closed)
// @Param			after	query		int		false	"Posição depois da qual a página começa (`next` da página anterior)"
// @Param			afterAt	query		string	false	"Nos pendentes, o `nextAt` da página anterior (RFC 3339)"
// @Param			asOf	query		string	false	"Nos pendentes, o `asOf` da primeira página (RFC 3339); obrigatório depois dela"
// @Param			limit	query		int		false	"Itens por página, até 200"
// @Success		200		{object}	CallListItemPageResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-lists/{listId}/items [get]
func (h *Handler) Items(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	state := calllist.State(strings.TrimSpace(r.URL.Query().Get("state")))
	after, validAfter := positiveQuery(r, "after")
	limit, validLimit := positiveQuery(r, "limit")
	afterAt, validAfterAt := instantQuery(r, "afterAt")
	asOf, validAsOf := instantQuery(r, "asOf")
	if (state != "" && !state.Valid()) || !validAfter || !validLimit || !validAfterAt || !validAsOf {
		response.WriteErrorWithCode(w, http.StatusBadRequest, "call_list_items_query_invalid",
			"state must be pending, reserved or closed; after and limit must be whole numbers; afterAt and asOf must be RFC 3339 instants", nil)
		return
	}
	q := calllist.ItemQuery{ListID: mux.Vars(r)["listId"], State: state, AfterPosition: after, AfterAt: afterAt, Limit: limit}
	if asOf != nil {
		q.AsOf = *asOf
	}
	rows, err := h.service.Items(r.Context(), actor, q)
	if err != nil {
		writeError(w, err)
		return
	}
	out := CallListItemPageResponse{Items: make([]CallListItemResponse, 0, len(rows.Items)), Next: rows.Next, NextAt: rows.NextAt, AsOf: rows.AsOf}
	for _, row := range rows.Items {
		out.Items = append(out.Items, rowResponseOf(row))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Próximo contato da lista
// @Description	Reserva para você o próximo item da lista e o devolve com o que é preciso para ligar. A fila serve primeiro os retornos que já venceram (`_callback` com `callbackAt` no passado), depois os itens cuja reserva expirou e depois os pendentes pela posição. Cada membro segura um item por vez: pedir de novo devolve o mesmo item e renova a reserva (15 minutos, renovada também quando a ligação começa); uma reserva sua que já expirou volta para a fila antes. Se você segura um item de outra lista, responde 409 `call_list_reservation_held`.
// @Description
// @Description	Antes de entregar, a regra de ligação é conferida de novo. Um lead que foi bloqueado, pediu para não ser chamado, perdeu o número ou tem um número inválido tem o item fechado com `_refused` e o motivo em `refusal`. O próximo é servido, e `refused` conta quantos foram pulados. A cada pedido são pulados no máximo 25: quando o limite é atingido sem item, `more` vem `true` e é preciso pedir de novo.
// @Description
// @Description	A resposta traz `item`, `lead` (só nome, bairro, cidade e quantidade de familiares), `lastInteraction` (a conversa mais recente do lead, só quando você pode abri-la) e as linhas que podem fazer a ligação (`trunks`, ou `trunkRefusal`: `unauthorized` ou `no_dialable_trunk`). Para ligar, use `start_call` no WebSocket de chamadas com `lead_id`, `phone_number` igual ao `phone` do item e `call_list_item_id`. O item traz `closable`: depois de recarregar a página com a ligação já feita, `true` diz que o resultado pode ser registrado sem ligar de novo. Sem item e com `more` `false`, a fila acabou por agora (`item` não vem). Exige ser um dos membros que ligam na lista e `call_lists:read`, `sip_trunks:read`, `sip_trunks:call` e `call_session:use`; a lista precisa estar `active`.
// @Tags			Listas de ligação
// @Produce		json
// @Param			listId	path		string	true	"ID da lista"
// @Success		200		{object}	CallListNextResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-lists/{listId}/next [post]
func (h *Handler) Next(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	next, err := h.service.Next(r.Context(), actor, mux.Vars(r)["listId"])
	if err != nil {
		writeError(w, err)
		return
	}
	if next == nil || next.List == nil {
		writeError(w, calllist.ErrListNotFound)
		return
	}
	response.WriteSuccess(w, http.StatusOK, nextResponseOf(next))
}

// @Summary		Liberar um item da lista
// @Description	Devolve o item que você reservou para a fila, sem resultado, para outro membro pegar (por exemplo quando a ligação nem chegou a começar). Só quem reservou libera (409 `call_list_item_not_reserved`). O item devolvido traz `closable`: quem já ligou para ele ainda pode registrar o resultado enquanto ninguém o pegar.
// @Tags			Listas de ligação
// @Produce		json
// @Param			itemId	path		string	true	"ID do item"
// @Success		200		{object}	CallListItemResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-list-items/{itemId}/release [post]
func (h *Handler) Release(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	verdict, err := h.service.Release(r.Context(), actor, mux.Vars(r)["itemId"])
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, verdictResponseOf(verdict))
}

// @Summary		Registrar o resultado de um item
// @Description	Fecha o item com um resultado do catálogo de resultados do workspace (o mesmo do encerramento de conversas) e uma nota de até 2.000 caracteres. `_callback` não fecha: devolve o item para a fila em `callbackAt` (no futuro, até 90 dias). A ligação conferida é a última que o servidor registrou no item quando você ligou com `call_list_item_id`; ela precisa ser deste workspace, para o lead do item e feita por você (409 `call_list_call_not_the_items`). Sem ligação registrada responde 409 `call_list_item_not_called`. Quem reservou fecha; depois que a reserva expira, quem fez a ligação ainda fecha, desde que ninguém tenha pegado o item (409 `call_list_item_taken`). Um workspace sem catálogo de resultados responde 422 `call_list_no_outcomes`; um código fora do catálogo, 400 `call_list_disposition_unknown`. O item devolvido traz `closable` `false`.
// @Tags			Listas de ligação
// @Accept			json
// @Produce		json
// @Param			itemId	path		string						true	"ID do item"
// @Param			body	body		CloseCallListItemRequest	true	"Resultado, nota e horário do retorno"
// @Success		200		{object}	CallListItemResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-list-items/{itemId}/close [post]
func (h *Handler) Close(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var body CloseCallListItemRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	verdict, err := h.service.Close(r.Context(), actor, mux.Vars(r)["itemId"], calllist.Closing{
		Disposition: body.Disposition, Note: body.Note, CallbackAt: body.CallbackAt,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, verdictResponseOf(verdict))
}
