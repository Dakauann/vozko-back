package lead

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	leaddomain "vozko/domain/lead"
	"vozko/domain/shared"
	lead_usecase "vozko/usecases/lead"
)

func entryTypeParam(r *http.Request) shared.EntryType {
	return shared.EntryType(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("entryType"))))
}

// @Summary		Conversa por entrada
// @Description	Retorna as mensagens de uma conversa, identificada pelo tipo e pelo ID da entrada. Responde 404 quando a conversa é de outro workspace ou está fora do que você pode abrir (outro departamento, ou de outra pessoa quando você não pode ver as conversas dos colegas).
// @Tags			Leads
// @Produce		json
// @Param			entryId		path		string	true	"ID da entrada"
// @Param			entryType	query		string	false	"Tipo da entrada ('whatsapp')"
// @Success		200	{object}	map[string]interface{}
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/entries/{entryId}/conversation [get]
func (h *LeadHandler) GetConversationByEntry(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requestActor(w, r)
	if !ok || !h.historyReady(w) {
		return
	}
	entryID := strings.TrimSpace(mux.Vars(r)["entryId"])
	if entryID == "" {
		response.WriteError(w, http.StatusBadRequest, "Entry ID is required", nil)
		return
	}
	found, err := h.history.EntryConversation(a, entryID, entryTypeParam(r))
	if err != nil {
		if errors.Is(err, lead_usecase.ErrConversationNotVisible) {
			response.WriteErrorWithCode(w, http.StatusNotFound, "conversation_not_found", "Conversation not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch conversation", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"entryId":      found.EntryID,
		"entryType":    found.EntryType,
		"leadId":       found.LeadID,
		"campaignId":   found.CampaignID,
		"status":       found.Status,
		"messages":     found.Messages,
		"messageCount": len(found.Messages),
	})
}

// @Summary		Entradas do lead por campanha
// @Description	Retorna as conversas do lead em uma campanha, só as que você pode abrir. Responde 404 quando o lead é de outro workspace.
// @Tags			Leads
// @Produce		json
// @Param			id			path		string	true	"ID do lead (UUID)"
// @Param			campaignId	path		string	true	"ID da campanha"
// @Param			entryType	query		string	false	"Tipo da entrada ('whatsapp')"
// @Success		200	{object}	map[string]interface{}
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/campaigns/{campaignId}/entries [get]
func (h *LeadHandler) GetEntriesByCampaign(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requestActor(w, r)
	if !ok || !h.historyReady(w) {
		return
	}
	leadID, campaignID := mux.Vars(r)["id"], strings.TrimSpace(mux.Vars(r)["campaignId"])
	if campaignID == "" {
		response.WriteError(w, http.StatusBadRequest, "Campaign ID is required", nil)
		return
	}
	visible, err := h.history.EntriesInCampaign(a, leadID, campaignID, entryTypeParam(r))
	if err != nil {
		writeLeadReadError(w, err)
		return
	}
	entries := make([]EntryResponse, 0, len(visible))
	for _, e := range visible {
		entries = append(entries, EntryResponse{
			ID:         e.ID,
			CampaignID: e.CampaignID,
			EntryType:  shared.EntryTypeWhatsApp,
			Status:     string(e.Status),
			CreatedAt:  fmtRFC3339(e.CreatedAt),
		})
	}
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"leadId":     leadID,
		"campaignId": campaignID,
		"entries":    entries,
	})
}

// @Summary		Análises do lead por campanha
// @Description	Retorna as análises de IA das conversas do lead em uma campanha, só das conversas que você pode abrir. Responde 404 quando o lead é de outro workspace.
// @Tags			Leads
// @Produce		json
// @Param			id			path		string	true	"ID do lead (UUID)"
// @Param			campaignId	path		string	true	"ID da campanha"
// @Param			entryType	query		string	false	"Tipo da entrada ('whatsapp')"
// @Success		200	{object}	map[string]interface{}
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/campaigns/{campaignId}/analysis [get]
func (h *LeadHandler) GetAnalysisByCampaign(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requestActor(w, r)
	if !ok || !h.historyReady(w) {
		return
	}
	leadID, campaignID := mux.Vars(r)["id"], strings.TrimSpace(mux.Vars(r)["campaignId"])
	if campaignID == "" {
		response.WriteError(w, http.StatusBadRequest, "Campaign ID is required", nil)
		return
	}
	l, analyses, err := h.history.Analyses(r.Context(), a, leadID, campaignID, entryTypeParam(r))
	if err != nil {
		writeLeadReadError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"leadId":     l.ID,
		"campaignId": campaignID,
		"number":     l.Number,
		"name":       l.Name,
		"analyses":   analyses,
	})
}

// @Summary		Ficha do lead de uma conversa
// @Description	Retorna a ficha do lead para o painel da conversa: nome, número de WhatsApp, `version`, bloqueio, responsável (`owner` e o nome em `ownerName`; se o nome não puder ser lido, a resposta é 500), o pedido para não receber mensagens (`optedOutAt` e `optOutSource`, ausentes quando o lead nunca pediu), bairro e cidade do endereço principal (`area`), os campos personalizados (os sensíveis só para quem tem `leads:read_sensitive`) e as contagens de família (`relativesCount`) e de indicações (`referredCount`). Basta poder abrir a conversa: não exige `leads:read`, e o endereço completo nunca vem aqui. Responde 404 `conversation_not_found` quando a conversa está fora do que você pode abrir e 404 `lead_not_found` quando a conversa não tem lead.
// @Tags			Leads
// @Produce		json
// @Param			entryId		path		string	true	"ID da entrada (UUID)"
// @Param			entryType	query		string	false	"Tipo da entrada ('whatsapp', 'unofficial_whatsapp', 'instagram', 'telegram', 'facebook', 'webchat')"
// @Success		200	{object}	lead.LeadCardResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/entries/{entryId}/lead [get]
func (h *LeadHandler) GetLeadByEntry(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requestActor(w, r)
	if !ok || !h.historyReady(w) {
		return
	}
	card, err := h.history.EntryLead(r.Context(), a, mux.Vars(r)["entryId"], entryTypeParam(r))
	if err != nil {
		if errors.Is(err, lead_usecase.ErrConversationNotVisible) {
			response.WriteErrorWithCode(w, http.StatusNotFound, "conversation_not_found", "Conversation not found", nil)
			return
		}
		writeLeadReadError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, leadCardResponse(card))
}

// @Summary		Resumo do lead para as abas e os telefones
// @Description	Retorna o que as abas e os telefones da ficha do lead mostram, fora de GET /leads/{id} para que uma falha aqui não derrube a ficha: `dealsCount` (negócios do lead dentro do que você pode ver no funil; ausente quando você não pode ver negócios, e 0 quando pode e não há nenhum), `memoriesCount` (memórias do lead) e `sharedNumbers`. `sharedNumbers` lista, para cada número do lead que outro lead também tem (o WhatsApp do lead ou um telefone de contato, em `number` exatamente como está guardado no lead), até 5 desses outros leads (`holders`, com ID, nome e WhatsApp), em ordem de nome sem acentos e os sem nome por último; `more` diz que há mais. As duas formas de um número (com e sem o nono dígito) contam como o mesmo número. Quando muitos leads têm o mesmo número, os 5 saem de uma amostra fixa (os primeiros pelo ID do lead), sempre a mesma a cada leitura, e não são necessariamente os primeiros pelo nome. Números que só este lead tem não aparecem, e a lista vem vazia quando não há nenhum. Exige `leads:read`. Códigos: 403 `forbidden`; 404 `lead_not_found`; 503 `lead_history_unavailable`.
// @Tags			Leads
// @Produce		json
// @Param			id	path		string	true	"ID do lead (UUID)"
// @Success		200	{object}	lead.LeadDetailSummaryResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/summary [get]
func (h *LeadHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requestActor(w, r)
	if !ok {
		return
	}
	if h.summaries == nil {
		writeHistoryUnavailable(w)
		return
	}
	summary, err := h.summaries.Summary(r.Context(), a, mux.Vars(r)["id"])
	if err != nil {
		writeLeadReadError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, leadDetailSummaryResponse(summary))
}

// @Summary		Família e indicações do lead
// @Description	Lista, página por página, as relações do lead com outros leads, mais antigas primeiro. `kind` diz o que o outro lead é deste lead e `dimension` é `family` ou `referral`; com `dimension` a lista traz só uma das duas. Leads excluídos não aparecem. Para a próxima página, envie o `next` recebido em `after`; sem `next`, a lista acabou. Exige `leads:read`. Códigos: 400 `lead_relatives_query_invalid` (dimensão desconhecida, tamanho negativo ou `after` que não veio daqui); 404 `lead_not_found`.
// @Tags			Leads
// @Produce		json
// @Param			id			path		string	true	"ID do lead (UUID)"
// @Param			dimension	query		string	false	"family ou referral"
// @Param			after		query		string	false	"Cursor `next` da página anterior"
// @Param			limit		query		int		false	"Itens por página (padrão 50, máximo 100)"
// @Success		200	{object}	lead.RelativesPageResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/relatives [get]
func (h *LeadHandler) ListRelatives(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requestActor(w, r)
	if !ok || !h.historyReady(w) {
		return
	}
	query := r.URL.Query()
	q := leaddomain.RelativesQuery{
		LeadID:    mux.Vars(r)["id"],
		Dimension: leaddomain.RelationDimension(query.Get("dimension")),
		After:     query.Get("after"),
	}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			response.WriteErrorWithCode(w, http.StatusBadRequest, leaddomain.ErrorCode(leaddomain.ErrRelativesQueryInvalid), leaddomain.ErrRelativesQueryInvalid.Error(), nil)
			return
		}
		q.Limit = limit
	}
	page, err := h.history.Relatives(r.Context(), a, q)
	if err != nil {
		writeLeadReadError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, relativesPageResponse(page, q.LeadID))
}
