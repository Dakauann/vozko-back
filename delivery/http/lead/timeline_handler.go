package lead

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	leaddomain "vozko/domain/lead"
	"vozko/domain/opportunity"
	lead_usecase "vozko/usecases/lead"
)

type Timeline interface {
	Page(ctx context.Context, a lead_usecase.Actor, q leaddomain.PageQuery) (leaddomain.TimelinePage, error)
	Deals(ctx context.Context, a lead_usecase.Actor, q leaddomain.PageQuery) (lead_usecase.DealsPage, error)
}

func (h *LeadHandler) timelineRequest(w http.ResponseWriter, r *http.Request) (lead_usecase.Actor, leaddomain.PageQuery, bool) {
	a, ok := h.requestActor(w, r)
	if !ok {
		return lead_usecase.Actor{}, leaddomain.PageQuery{}, false
	}
	if h.timeline == nil {
		writeHistoryUnavailable(w)
		return lead_usecase.Actor{}, leaddomain.PageQuery{}, false
	}
	query := r.URL.Query()
	q := leaddomain.PageQuery{LeadID: mux.Vars(r)["id"], Before: query.Get("before")}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			response.WriteErrorWithCode(w, http.StatusBadRequest, leaddomain.ErrorCode(leaddomain.ErrPageQueryInvalid), leaddomain.ErrPageQueryInvalid.Error(), nil)
			return lead_usecase.Actor{}, leaddomain.PageQuery{}, false
		}
		q.Limit = limit
	}
	return a, q, true
}

// @Summary		Linha do tempo do lead
// @Description	Lista, da mais recente para a mais antiga e página por página, tudo o que aconteceu com o lead: conversas em todos os canais (`conversation`, na primeira mensagem de cada conversa; um envio de campanha que nunca saiu e uma conversa aberta só pela importação não aparecem), envios de campanha oficiais e não oficiais (`campaign_sent`, `campaign_delivered`, `campaign_read`, `campaign_failed`; marcados desde 2026-09-25), ligações (`call`, pelo lead ou, nas mais antigas, pelo número de WhatsApp do lead; telefones de contato, que outras pessoas podem dividir, não servem; uma ligação feita por uma lista de ligação traz `callListId` e, depois que o operador registra o resultado, `disposition` com o código do resultado no catálogo do workspace, ou `_callback` com `callbackAt` quando ficou para retornar (sem `callbackAt` quando a lista adiou o contato por uma recusa temporária, porque essa data é da lista e não do operador); uma recusa da lista não é resultado da ligação e não aparece; `callListId`, `disposition` e `callbackAt` só vêm para quem vê aquela lista, com `call_lists:read` e sendo um dos responsáveis por ela, ou gerenciando listas com `call_lists:manage`; para os demais a ligação aparece sem esses campos), negócios (`deal`, os que têm este lead e os ligados a uma conversa dele, sem repetir), movimentos desses negócios (`deal_event`, com `event` `stage_moved`, `won`, `lost` ou `reopened`, `stageName` e `fromStageName`, no mesmo escopo de negócios), memórias (`memory`) e alterações do cadastro (`record`, só com o nome de cada campo alterado, nunca os valores; um campo sensível só é nomeado para quem tem `leads:read_sensitive`, e uma alteração só de campos que você não vê não aparece). Cada item passa pela mesma regra de acesso da sua origem: conversas e envios só aparecem quando você pode abrir a conversa (outro departamento, ou de outra pessoa quando você não pode ver as conversas dos colegas, não aparecem); ligações só com `call_history:read`, e só as suas a menos que você tenha `call_history:view_others`; negócios só dentro do seu escopo de negócios. `ref` aponta para o que abrir: `entry` (com `entryType`), `call` (o `callId` de GET /calls/{callId}), `deal` (o negócio, também num `deal_event`), `memory` ou `lead_event`. Para a próxima página, envie o `next` recebido em `before`; sem `next`, a linha do tempo acabou. Uma página pode vir com menos itens que `limit` (até vazia) e ainda trazer `next`, quando muitos itens seguidos não são seus: continue pedindo. Substitui GET /leads/{id}/conversations, que foi removida. Exige `leads:read`. Códigos: 400 `lead_page_invalid` (`limit` negativo ou que não é número, `before` que não veio desta rota); 403 `forbidden`; 404 `lead_not_found`; 503 `lead_history_unavailable`.
// @Tags			Leads
// @Produce		json
// @Param			id		path		string	true	"ID do lead (UUID)"
// @Param			before	query		string	false	"Cursor `next` da página anterior"
// @Param			limit	query		int		false	"Itens por página (padrão 30, máximo 100)"
// @Success		200	{object}	lead.LeadTimelineResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/timeline [get]
func (h *LeadHandler) Timeline(w http.ResponseWriter, r *http.Request) {
	a, q, ok := h.timelineRequest(w, r)
	if !ok {
		return
	}
	page, err := h.timeline.Page(r.Context(), a, q)
	if err != nil {
		writeLeadReadError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, timelineResponse(q.LeadID, page))
}

// @Summary		Negócios do lead
// @Description	Lista, do mais recente para o mais antigo e página por página, os negócios (atendimentos) do lead: os que têm este lead e os ligados a uma conversa dele, sem repetir. Um negócio criado para o lead sem nenhuma conversa também aparece. Só entram os negócios dentro do seu escopo (os mesmos que o funil mostra a você). Para criar um novo, use POST /opportunities com `leadId` (a conversa é opcional; quando só a conversa é enviada, o lead é o dessa conversa). Para a próxima página, envie o `next` recebido em `before`. Exige `leads:read` e acesso aos negócios. Códigos: 400 `lead_page_invalid` (`limit` negativo ou que não é número, `before` que não veio desta rota, como um cursor da linha do tempo); 403 `forbidden` ou `deals_forbidden` (sem acesso aos negócios do workspace); 404 `lead_not_found`; 503 `lead_history_unavailable`.
// @Tags			Leads
// @Produce		json
// @Param			id		path		string	true	"ID do lead (UUID)"
// @Param			before	query		string	false	"Cursor `next` da página anterior"
// @Param			limit	query		int		false	"Negócios por página (padrão 30, máximo 100)"
// @Success		200	{object}	lead.LeadDealsResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/deals [get]
func (h *LeadHandler) Deals(w http.ResponseWriter, r *http.Request) {
	a, q, ok := h.timelineRequest(w, r)
	if !ok {
		return
	}
	page, err := h.timeline.Deals(r.Context(), a, q)
	if errors.Is(err, opportunity.ErrScopeDenied) {
		response.WriteErrorWithCode(w, http.StatusForbidden, "deals_forbidden", "Você não tem acesso aos negócios deste workspace", nil)
		return
	}
	if err != nil {
		writeLeadReadError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, dealsResponse(q.LeadID, page.Deals, page.Next))
}
